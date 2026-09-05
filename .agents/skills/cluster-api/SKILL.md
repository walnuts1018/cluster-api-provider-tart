---
name: cluster-api
description: TartのCluster API Provider contractとResource実装を確認する
when_to_use: CAPI Resource、Provider contract、Controller、ClusterClass、Runtime Extensionを実装・レビューする時
---

# Cluster API実装方針

Cluster APIのResourceとcontrollerを変更する前に、[公式Provider documentation](https://cluster-api.sigs.k8s.io/developer/providers/overview)と、このリポジトリの[API contract](../../../docs/development/api-contract.md)を確認する。CAPI contractの仕様を推測で実装せず、依存しているCAPI versionの型とcontractを確認する。

## VersionとAPI group

Tart独自APIは次のgroup/versionへ分けて`v1alpha1`へリセットする。

- Infrastructure: `infrastructure.cluster.x-k8s.io/v1alpha1`
- Bootstrap: `bootstrap.cluster.x-k8s.io/v1alpha1`
- Control Plane: `controlplane.cluster.x-k8s.io/v1alpha1`

これはCAPI coreのversionではない。CAPI core resourceと現行contractは実装時点の`cluster.x-k8s.io/v1beta2`へ合わせる。contractへ参加するprovider CRDには`cluster.x-k8s.io/v1beta2: v1alpha1` labelを付ける。旧Tart `v1beta1`とのconversionや互換性は作らない。別groupのprovider resourceをCAPI coreが参照できるようaggregated RBACを生成する。

## Spec、Status、reference

- `Spec`は利用者またはCAPIが宣言したdesired state、`Status`はTart、Talos、workload clusterから観測したactual stateとConditionsだけを持つ。CAPI-facing Resourceの安全停止は汎用`Blocked` Condition typeを増やさず、`Ready=False`または`Available=False`と固定したreasonで表す。
- Statusの`observedGeneration`は対応するSpec generationを反映する。workflow phase、step番号、retry counterをStatusへ保存しない。
- Infrastructure Clusterはcontrol plane endpoint、provisioned、failure domains、Conditionsをcontractに従って公開する。failure domainをallocationまで接続できない間はsurfaceしない。
- Infrastructure Machineは`spec.providerID`、`status.initialization.provisioned`、`status.addresses`、failure domain、Conditionsをcontractに従って扱う。Node `spec.providerID`と必ず一致させる。
- Bootstrap Configは`status.initialization.dataSecretCreated`、`status.dataSecretName`、Conditionsを公開し、Secret dataをStatusへコピーしない。
- Control Planeは`spec.version`、`spec.replicas`、`spec.machineTemplate.spec.infrastructureRef`、`spec.machineTemplate.spec.deletion`、provider-specificな`spec.bootstrapConfigTemplate`、`status.versions`、`status.initialization.controlPlaneInitialized`、`replicas`、`readyReplicas`、`availableReplicas`、`upToDateReplicas`、`selector`、scale subresource、workload kubeconfig Secretをcontractに従って扱う。`status.version`は新設しない。`nodeDrainTimeoutSeconds`、`nodeVolumeDetachTimeoutSeconds`、`nodeDeletionTimeoutSeconds`は`spec.machineTemplate.spec.deletion`へ置く。
- `controlPlaneInitialized`はAPI serverがrequestを受け付ける状態を表し、全Node ReadyやCNI導入を待たない。
- `TartHost`はCAPI Machineより長寿命なので、CAPI MachineのOwnerReferenceを設定しない。`TartHost.spec.id`はKubernetes metadata UIDから独立したimmutableな永続Host identityとし、`TartCluster.spec.id`もCAPI `Cluster.metadata.uid`から独立したimmutableなworkload cluster identityとする。`TartMachine`とBootstrap resourceは対応するCAPI resourceとのOwnerReferenceとCAPI labelを正しく設定する。
- `TartHost.spec.id`と`TartCluster.spec.id`はTemplateやSSA dry-runのdefaultingで生成しない。通常CREATEでは空値をprovider controllerがnon-dry-run CREATE後に一度だけ生成して永続化し、指定済みのIDは拒否する。DR復元では`tart.cluster.x-k8s.io/restore-approved: "true"` annotationとinfra administratorの権限境界を満たす場合だけ既存値を保持する。ID確定前にbundle生成、Host claim、provisioningを開始しない。
- `TartClusterTemplate.spec.template.spec`に`id`を持たせず、`updatePolicy.allowDowntime`だけをconcrete Clusterへ伝播可能なcluster-level policyとして扱う。`TartCluster.spec.updatePolicy.allowDowntime: true`はavailability、PDB、capacityによるdrain失敗を緩和する正本であり、未指定または`false`ならavailability理由でも開始しない。destructive disk change、identity mismatch、Host mismatch、unsafe etcd membership change、quorum violationは緩和しない。
- CAPI contractへ参加するResourceはnamespace-scopedとし、`TartHost`だけはmanagement cluster全体で一意なcluster-scoped inventoryとする。Claim中またはRetainedの`TartHost`直接削除は、現在のbindingまたはretained recordに一致する`spec.forgetApproval`なしに許可せず、forgetしてもpower off、reset、disk wipeを行わない。

## Host allocation

`TartHost.spec.consumerRef`をcontroller-managed desired bindingとしてatomic CASで管理し、SSAのfield ownershipをlockとして使わない。`GET → consumerRefがnilまたは自分のUIDであることを確認 → resourceVersion付きUpdate`またはJSON Patchの`test`でclaimする。`TartHost.status`をlockの正本にしない。Machine削除後は`TartHost.spec.retainedFrom`へ直前のconsumer UIDと`TartCluster.spec.id`由来のcluster IDを永続的に記録する。`TartMachine.status.hostRef`はbindingの観測である。現在の`retainedFrom`に一致する`reuseApproval`と`Adopt`/`Reprovision` modeが明示されるまでHostをselectorの候補に戻さない。再利用承認は成功時にSpecから消費せず、次の`retainedFrom.uid`が変わることで自然に無効化する。

`Machine.spec.failureDomain`が指定されている場合はHost allocatorが一致するfailure domainを必ず選ぶ。Host停止を確認できない削除ではclaimを解除せず、finalizerを保持して`Ready=False`、`Reason=ShutdownUnconfirmed`にする。

## Bootstrap Secretとcluster secret

Bootstrap Secretは決定論的な名前（初期実装ではBootstrapConfig名）、type `cluster.x-k8s.io/secret`、single `value` key、cluster label、BootstrapConfigのcontroller OwnerReferenceを持つ。`value`にはcomplete Talos machine configurationだけを格納する。Talos/Kubernetes cluster secret bundleはCluster IDを含むgeneration単位でimmutable Secretを生成し、active generationの観測を`TartCluster.status.activeSecretGeneration`へ反映する。CA rotationではgeneration Nを基にrotation対象のTalos/Kubernetes CAだけを更新した完全なgeneration N+1 bundleを`Pending` SecretとしてTalos operation開始前に永続化し、Talos公式のaccepted/issuing CAとcertificate refreshのsemanticsをmachine configuration/APIでreconcileする。自動`rotate-ca`を完了後にmaterial回収するブラックボックスとして扱わず、Pending bundleとobserved stateから再開する。generation N+1でrotation対象外のetcd CA、service account keyなどを意図せず変更しない。正常完了を観測してから新generationをactiveに確定し、Cluster存続中は過去generationをGCしない。BootstrapConfigごとにcluster secretsをgenerateしない。

ユーザーのraw configuration patchは全て`TartBootstrapConfig.spec.configSecretRef`のSecret-backed inputへ格納し、inline fieldは提供しない。参照先Secretは`immutable: true`を必須とし、Secretには非機密configurationを含めてもよい。同じSecretの内容だけを書き換えてBootstrapConfigのdesired diffを隠してはならない。`CanUpdateMachineSet`と`CanUpdateMachine`はold/new双方のSecretをresolveしてeffective configurationをrenderし、Secret参照名だけでなくsemantic diff全体を判定する。Bootstrap Secret生成後のmutableな変更はUpdate Extensionだけが実行し、Bootstrap controllerは再生成しない。

Control Plane ProviderはCluster namespaceへ`<cluster-name>-kubeconfig` Secretを生成・維持する。type、label、single `value` key、TartControlPlaneのcontroller OwnerReference、client certificateの更新をCAPI contractに合わせる。Control Plane endpointのVIPをTartがallocateする責務は持たず、`Cluster.spec.controlPlaneEndpoint`が設定されるまで必要なreconcileを待つ。

## Controller

Reconcileは、Kubernetes desired state、TartHost observed state、Talos API observed state、必要なworkload cluster observed stateから毎回次の安全なactionを判断する。controller再起動後にprocess memoryや独自Operation resourceがなくても継続できることを確認する。

Resourceの作成と通常のspec管理にはserver-side applyを使う。StatusにはStatus subresourceへのserver-side applyまたはpatchを使う。field managerを責務ごとに固定し、ユーザー、CAPI core、別providerのfieldを上書きしない。`Create`や通常の`Update`でresource全体を上書きしない。ただし`TartHost.spec.consumerRef`だけはSSAを使わず、resourceVersion付きUpdateまたはJSON Patchのatomic CASで排他claimする。

Transient errorはrequeueし、identity mismatch、unsafe storage change、quorumを守れないscale down、unsupported update path、shutdown未確認のdeletionは`Ready=False`と具体的なreasonへ反映する。Machine deletionのdrainとvolume detachはCAPI Machine controller、scale-down時のetcd detachはControl Plane Providerのpre-terminate delete hook、Talos shutdown、停止確認、retention、claim解除は`TartMachine` finalizerが担当する。Cluster全体の削除ではetcd quorum維持を必須にせず、disk wipeもしない。

## Provider contract

Infrastructure、Bootstrap、Control Planeの各Providerは、CAPIが読むreference、labels、OwnerReference、readiness Conditions、deletion semanticsを満たす。ClusterClassからtemplate resourceを通常のCAPI resourceとして参照できるようにし、Tart専用installation pathを要求しない。

Control Planeのreplica、Kubernetes version、etcd membership、cluster secret bundle、workload kubeconfigはControl Plane Providerが所有する。cluster secret bundleはCluster IDを含むgeneration単位でimmutableに生成し、active generationの観測を`TartCluster.status.activeSecretGeneration`へ反映する。CA rotationではrotation対象のCAだけを更新した次generationのPending Secretを先に永続化し、Talos公式の段階的semanticsをmachine configuration/APIでreconcileする。自動`rotate-ca`をブラックボックスとして扱わず、rotation完了観測後にだけactive generationを確定する。Cluster存続中は過去generationをGCしない。Bootstrap Providerはread-onlyで参照し、Infrastructure Providerがcontrol planeのquorumやadd-onを管理しない。CNI、CSI、kube-vip、observabilityなどはTalos configurationまたはKubernetes addon layerへ委譲する。

## Runtime Extensionとrollout

CAPIのin-place update hookを使う場合、management clusterで`RuntimeSDK=true`と`InPlaceUpdates=true`を有効にし、HTTPS endpointを`ExtensionConfig`へ登録する。TLS Secret、server certificate、必要なCA trustを管理する。現行CAPIではin-place update hookへ登録できるextensionは1つなので、deployment前に他extensionとの競合がないことを確認する。

`CanUpdateMachineSet`、`CanUpdateMachine`、`UpdateMachine`を一体のRuntime Extension contractとして実装する。前者はMachineDeploymentのMachineSet差分、中央は個別Machine差分を判定し、後者は同じMachineへTalos operationを適用する。`configSecretRef`はold/new双方を解決してeffective configurationをrenderし、Secret参照名だけでなくsemantic diff全体を判定する。Secretがmissing、unreadable、generation不明ならunknownとして、unsafe、unknown、partial diffはpatchなしの`Failure`で止める。`Failure`を「別のProviderが扱える」という意味で解釈せず、Tartではこの差分をvetoする契約とする。CAPI minorごとに、unsafe diffでもMachineSet、Machine、TartHost claimが一つも作られないことを必須E2Eで確認する。初回provisioning後のmutableなTalos OS/config変更を実行するのはUpdate Extensionだけで、Infrastructure/Bootstrapの通常reconcileは観測とStatus反映を行う。Workerの標準rollout profileは対応するCAPI設定で`maxSurge: 0`、`maxUnavailable: 1`とし、`OnDelete`は自動worker in-place update lifecycleとしてサポートしない。Control Planeのin-place updateはTartControlPlaneが`CanUpdateMachine`を呼んで一台ずつ遷移させる。Tart独自のrollout controllerは作らない。

## SecretとMHC

Talos machine secrets、Kubernetes PKI private key、Talos client key、Bootstrap Data、kubeconfig、BMC credentialはStatus、Event、通常log、metrics labelへ出さない。Bootstrap dataはBootstrap ProviderがSecretへ格納し、Infrastructure Providerは必要なdataを取得してTalos APIへ渡すだけにする。

Tartはlocal volumeやadd-onの有無を安全に判定できないため、すべてのTart-managed MachineでMHCのdelete-and-recreate remediationを既定で許可しない。初期運用ではMachineSetまたはControl PlaneのMachine templateへ生成前から`cluster.x-k8s.io/skip-remediation`を設定し、Machine作成後の後追いannotationだけに依存しない。Tart v1alpha1では自動replacementやguided reprovisionのopt-inを提供しない。利用者のMachine削除はCAPIの通常replacement semanticsを発生させ得て、別のAvailable Hostをclaimする可能性がある。Retained Hostの`Reprovision`承認はdata破棄だけを許可し、Machine削除や同じHostへの再割り当てを開始しない。将来のexternal remediationは同じMachineを維持するpower cycle/Talos recovery方式とする。

## Control Plane deletionとupgrade

Control Planeのscale-downではpre-terminate delete hook（`pre-terminate.delete.hook.machine.cluster.x-k8s.io/...`）でetcd member removalとquorumを確認してからMachine deletionを進める。Cluster全体のdeletionではmember removalを必須にせず、hookを安全に完了させる。control planeのin-place updateは常に一台ずつとし、次のMachineへ進む前にetcd、Kubernetes API、Node healthを再観測する。node-disruptive operationの前にはTalosのdrain、またはworkload cluster側のcordon/drainを試す。drain失敗がavailability、PDB、capacityだけの理由で`TartCluster.spec.updatePolicy.allowDowntime: true`が明示されている場合はgraceful shutdown/rebootを許可し、未指定または`false`なら開始しない。destructive disk change、identity mismatch、Host mismatch、unsafe etcd membership change、quorum violationは緩和しない。
Tart v1alpha1では自動replacementやguided reprovisionのopt-inを提供しない。利用者のMachine削除はCAPIの通常replacement semanticsを発生させ得て、別のAvailable Hostをclaimする可能性がある。Retained Hostの`Reprovision`承認はdata破棄だけを許可し、Machine削除や同じHostへの再割り当てを開始しない。

## ClusterClassとSSA dry-run

ClusterClassをサポートする場合、Topology controllerがInfraMachineTemplateとBootstrapConfigTemplateへ行うSSA dry-runをprovider webhookが受け入れなければならない。dry-runではSecret、OwnerReference、Status、外部API副作用を作成せず、observed stateを前提にした検証や生成済みmetadataを要求しない。defaultingとvalidationはdry-runと実適用で同じ結果にし、templateから通常のCAPI resourceへ変換できるfieldだけを検証する。

Topology managed clusterではCAPI upgrade planのcontrol-plane/worker stepとversion skewがtarget versionに整合していれば、現在のworker desired versionが旧versionでもcluster-wide `upgrade-k8s`を開始できる。directly managed clusterでは`TartControlPlane.spec.version`の変更をtriggerにし、workerの`Machine.spec.version`またはMachineDeployment desired versionが目標versionと矛盾する場合だけ開始前に`Ready=False`、`Reason=VersionSkew`にする。どちらの場合もControl Plane Providerがworker resourceを変更しない。Talos `upgrade-k8s`はclusterごとに一度だけ要求し、これはMachineDeploymentの`maxUnavailable`ではなくTalosのcluster-wide semanticsへ委譲し、全Node actual versionを観測してからControl Plane statusを更新する。
