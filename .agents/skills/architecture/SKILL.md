---
name: architecture
description: TartのTalos専用アーキテクチャと責務境界を確認する
when_to_use: Resource、Provider、controller、外部adapterの設計・実装・レビューを行う時
---

# Tartアーキテクチャ

## 先に読む文書

設計・実装・レビューを始める前に、次の順で読む。

1. [`docs/development/README.md`](../../../docs/development/README.md)
2. [`docs/development/architecture.md`](../../../docs/development/architecture.md)
3. [`docs/development/api-contract.md`](../../../docs/development/api-contract.md)
4. [`docs/development/lifecycle.md`](../../../docs/development/lifecycle.md)
5. [`docs/development/talos.md`](../../../docs/development/talos.md)
6. [`docs/development/decisions.md`](../../../docs/development/decisions.md)
7. [`docs/development/verification.md`](../../../docs/development/verification.md)

各文書は責務ごとの設計の正本であり、`README.md`が参照関係の入口である。古いコード、旧API、旧ドキュメントに合わせるために新設計を曲げない。

## 基本方針

- TartはTalos Linux専用であり、Kubeadm、Ubuntu、汎用OS provisioning framework、既存Talos Providerには依存しない。
- Infrastructure、Bootstrap、Control PlaneのAPI groupを分けたProviderを提供する。
- TalosのOS installation、machine configuration、disk/volume、upgrade、rollback、etcd bootstrap、Kubernetes runtimeへ責務を委譲する。
- CAPI Machineを使い捨てと仮定せず、同じMachine、TartMachine、TartHost、diskを保ったin-place updateを第一選択にする。
- 安全にin-place updateできない変更はMachine replacementへ暗黙にfallbackせず、CAPI-facing Resourceでは`Ready=False`と安全なreasonで停止する。ただしCAPIのhook未対応差分がimmutable rolloutへfallbackし得るため、Host retention、rollout policy、MHC policyに加えてCAPI minorごとのfail-closed E2Eを必須とする。
- Resource Statusは外部から観測できる状態とConditionだけを持ち、workflowのstep番号やprogram counterを持たない。
- controller再起動後も、Kubernetes desired stateとHost/Talos/Kubernetesのobserved stateから同じ判断を再計算できるようにする。

## ディレクトリ方針

ルート直下の`internal`と`pkg`は禁止する。

## 安全性の不変条件

- `TartHost`はmanagement cluster全体で一意なcluster-scoped inventoryであり、immutableな`TartHost.spec.id`をKubernetes metadata UIDから独立した永続Host identityとして持つ。`TartHost.spec.consumerRef`がallocation bindingの正本であり、claimはSSAをlockとして使わず、resourceVersion付きUpdateまたはJSON Patchの`test`によるatomic CASで確立し、`TartHost.status`をlockの正本にしない。
- `TartHost.spec.id`と`TartCluster.spec.id`はTemplateやSSA dry-runのdefaultingで生成せず、通常CREATEでは空値をconcrete Resourceのnon-dry-run CREATE後にprovider controllerが一度だけ生成して永続化する。presetされたIDは通常CREATEで拒否し、DR復元では`tart.cluster.x-k8s.io/restore-approved: "true"` annotationとinfra administratorの権限境界を満たす場合だけバックアップ済みのIDを保持する。ID確定前にbundle生成、Host claim、provisioningを開始しない。同名Clusterの再作成では新しいCluster IDを使う。
- Machine削除後のHostは`spec.retainedFrom`を持つ`Retained`であり、現在のretained UIDに一致する明示的な`Adopt`または`Reprovision`承認なしに自動allocationしない。`Reusable`はwipeの同義語ではない。
- claim解放前にauthenticated Talos shutdownと停止確認を行い、確認不能ならclaimとfinalizerを保持して`Ready=False`とreasonを設定する。
- Tart-managed Machineはlocal persistent stateの有無を判定せず、MHC delete-and-recreate remediationを既定で許可しない。初期運用では`cluster.x-k8s.io/skip-remediation`を使い、replacementは明示的なopt-inに限定する。
- `TartCluster.spec.id`をCAPI `Cluster.metadata.uid`から独立したimmutableなworkload cluster identityとし、`retainedFrom.clusterID`、secret bundle、Adopt、DRの関連付けへ使う。同名Clusterの再作成で古いbundleやHost dataを再利用しない。`TartMachine.spec.talosImage`の`{version, schematicID}`をTalos image/system extensionの単一の正本にする。
- ProviderIDを`TartHost.spec.id`から`tart://host/<TartHost.spec.id>`としてHost allocation後に生成し、Talos kubeletへ注入してCAPI InfraMachine、TartMachine、Nodeの`spec.providerID`を一致させる。Kubernetes objectをバックアップから再作成してmetadata UIDが変わってもProviderIDを再構築できる。allocationはbootstrap dataを待たず、Discovery bootもbootstrap dataを待たず、Talos provisioningだけがbootstrap dataを待つ。
- 初回provisioning後のmutableなTalos OS/config updateはUpdate Extensionだけが実行し、通常のInfrastructure/Bootstrap reconcileは観測とStatus反映だけを行う。Control Plane Providerは`CanUpdateMachine`成功後にMachine、InfraMachine、BootstrapConfigをannotation付きで更新し、Machineへ`UpdateMachine` hook pendingを設定する。
- `controlPlaneInitialized`はAPI serverがrequestを受け付ける状態であり、全Node ReadyやCNI導入を待たない。
- cluster secret bundleはCluster IDを含むgeneration単位でimmutableに生成し、active generationの観測を`TartCluster.status.activeSecretGeneration`へ反映する。CA rotationではgeneration Nを基にrotation対象のTalos/Kubernetes CAだけを更新した完全なgeneration N+1 bundleを作成し、`Pending` Secretとして先に永続化する。その後、Talos公式のaccepted CA追加、issuing CA切替、certificate refresh、旧CA削除のsemanticsをTalos machine configuration/APIでreconcileする。自動`rotate-ca`をブラックボックスとして呼び出して完了後にmaterialを回収せず、Pending bundleとTalosのobserved accepted/issuing CAから再開する。正常完了を観測してから新generationをactiveに確定し、Cluster存続中は過去generationをGCせず、削除時にDR保持方針を確認した後だけGCを許可する。Bootstrap SecretとkubeconfigはCAPI Secret contractに合わせる。
- node-disruptive updateで停止を許可する正本は`TartCluster.spec.updatePolicy.allowDowntime: true`とする。未指定または`false`ならavailability理由でも安全停止する。`true`の場合はavailability、PDB、capacityに起因するdrain失敗だけを緩和し、destructive disk change、identity mismatch、Host mismatch、unsafe etcd membership change、quorum violationは緩和しない。Tart v1alpha1では自動replacementやguided reprovisionのopt-inを提供しない。

## 禁止事項

- `TartHostOperation`、Operation CRD、Workflow engine、Provisioning Planを追加しない。
- 独自Provisioning Agent、Node Lifecycle Agent、OS image format、disk writer、partition DSL、A/B updater、rollback managerを追加しない。
- Cilium、Longhorn、TopoLVM、kube-vipなどのadd-on専用APIを追加しない。
- DHCP、TFTP、PXE、BMC、VM APIの具体的な方式をTartのdomain modelやCRDへ固定しない。
- local persistent dataを持つ可能性があるHostを、通常のtemplate差分だけでcleaning、reprovisioning、disk wipeしない。
- CAPI webhookで`list TartHost → 重複がなければ許可`という非atomicな全体一意性検査を安全性の根拠にしない。重複identityを観測した場合は関係するHostを`Ready=False`、`Reason=IdentityConflict`としてallocationとmaintenance configuration applyを停止する。

## Discoveryとprovisioningの境界

- Enrollment/DiscoveryはBootstrapConfigとCAPI Machine provisioningから独立し、secret-freeなmaintenance Talos boot、hardware inventory取得、`TartHost.status`更新だけを行う。inventory未観測と観測済みはConditionで表し、Operation CRDを作らない。
- ProvisioningはHost claim後にBootstrap Secretを待ち、configuration apply、install、power操作を開始する。Discovery bootをBootstrap Secret待ちにしてはならない。
- machine configurationを持たないbare-metal Hostのmaintenance APIへ接続するときは、expected Host、boot attempt、endpoint、MAC/DHCP、observed identityを結び付け、曖昧ならfail closedにする。

## Updateの共通安全規則

- node-disruptiveなTalos operationの前にNodeをquiesceする。Talos operation自身が安全なdrainを提供する場合はそれを利用し、提供しない場合はworkload cluster側でcordon/drainする。drainが成功すればupdateを開始する。drain失敗がavailability、PDB、capacityだけの理由で`TartCluster.spec.updatePolicy.allowDowntime: true`が明示されている場合はgraceful shutdown/rebootを許可し、未指定または`false`なら安全停止する。destructive disk change、identity mismatch、Host mismatch、unsafe etcd membership change、quorum violationはavailability policyで緩和しない。
- Talosが旧versionへrollbackした場合はdesired Specを自動で旧versionへ戻さず、`UpdateMachine`を`Failure`、`Reason=RolledBack`としてControl Planeの次Machineへの更新を停止する。
- configuration digestはTalosが解釈したeffective machine configurationを正規化し、secret-bearing valueをredaction markerへ置換したsemantic representationのSHA-256とする。更新安全性は公開digestではなくold/new Secretを解決したsemantic diffで判定する。

## 変更時の確認

CRD、Provider contract、controllerの変更時は、設計の責務表、正本表、削除・更新の安全性、secret境界、controller再起動後の再計算可能性を確認する。詳細は[API contract](../../../docs/development/api-contract.md)、[Machine lifecycle](../../../docs/development/lifecycle.md)、[検証方針](../../../docs/development/verification.md)による。
