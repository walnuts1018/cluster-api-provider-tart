# API contract

TartのAPI schemaとCluster API contractの対応を定義する。Tart独自APIは`v1alpha1`へリセットし、CAPI coreの現行`v1beta2` contractへ接続する。過去のTart `v1beta1`とのconversionや互換層は作らない。

## API group

CAPIのproviderごとの責務に合わせてAPI groupを分ける。別groupのprovider resourceをCAPI coreが参照できるよう、生成するCRD、RBAC、aggregated roleに必要な権限を含める。

| Resource | Group/version |
| --- | --- |
| `TartHost`、`TartCluster`、`TartClusterTemplate`、`TartMachine`、`TartMachineTemplate` | `infrastructure.cluster.x-k8s.io/v1alpha1` |
| `TartBootstrapConfig`、`TartBootstrapConfigTemplate` | `bootstrap.cluster.x-k8s.io/v1alpha1` |
| `TartControlPlane`、`TartControlPlaneTemplate` | `controlplane.cluster.x-k8s.io/v1alpha1` |

ここでの`v1alpha1`はTart独自APIのversionであり、CAPI coreの`v1beta2`を意味しない。CAPI contractへ参加するprovider CRDには、contract version labelとして`cluster.x-k8s.io/v1beta2: v1alpha1`を付ける。

## Resource一覧

`TartHost`はCAPI外の長寿命Host inventoryであり、management cluster全体で一意なcluster-scoped Resourceとする。`TartCluster`、`TartMachine`、`TartBootstrapConfig`、`TartControlPlane`はそれぞれCAPIのInfrastructure Cluster、Infrastructure Machine、Bootstrap Config、Control Plane contractへ接続する。CAPI contractへ参加するResourceはnamespace-scopedとし、Template resourceは通常のCAPI template semanticsに従う。`TartHost`のMAC address、system UUID、その他のstable identityは全Host間で重複を拒否し、namespaceによる物理Hostの分割を許さない。

| Resource | 主な責務 |
| --- | --- |
| `TartHost` | Host identity、hardware inventory、power/boot capability、allocation eligibility |
| `TartCluster` | cluster-level infrastructure、control plane endpoint、failure domainの観測 |
| `TartMachine` | CAPI `Machine`とHostのbinding、Talos image、ProviderID、addresses、infrastructure readiness |
| `TartBootstrapConfig` | CAPI bootstrap dataとして適用可能なTalos machine configurationの生成 |
| `TartControlPlane` | control plane Machine、etcd bootstrap、kubeconfig、Kubernetes lifecycle、quorum safety |

Cluster topology、Machine version、rollout policy、add-onはCAPIまたはKubernetes側の正本とし、Tart resourceへ重複して持たせない。

## `TartHostSpec`

初期登録に必要なfieldはHost identityとpower/boot capabilityに限定する。MAC addressは主要なenrollment情報とし、system UUID、NIC名、disk UUID、Linux device path、Talos endpointの事前入力を必須にしない。architecture、label、failure domainはallocation条件として指定可能にするが、Talosから取得できるinventoryをユーザーのdesired hardware stateとして複製しない。

`spec.consumerRef`はcontrollerが管理するbindingであり、ユーザーが任意のMachineへ向ける通常設定ではない。claimはSSAのfield ownershipをlockとして使わず、`GET → consumerRefがnilまたは自分のUIDであることを確認 → 取得したresourceVersion付きUpdate`、またはJSON Patchの`test`を使うatomic CASで確立する。競合したUpdateは成功として扱わず、別Hostの選択または再試行を行う。`TartHost.status`をallocation lockの正本にせず、Statusには`Claimed` Conditionと観測されたconsumerを必要な範囲で保持する。

`spec.reusePolicy`の既定値は`Retain`とする。`Reusable`は単独のwipe許可ではなく、`spec.retainedFrom`に一致する明示的な再利用承認と、`spec.reuseMode`（`Adopt`または`Reprovision`）がそろった場合だけ有効になる。`Reusable`への変更はHostがすでに`Retained`になった後だけ許可し、Claim中または未使用Hostの時点で将来の削除を事前承認できないようにする。controllerはユーザーの再利用指定を自動生成しない。

`TartHostSpec`のretention関連fieldは次の責務を持つ。

```yaml
consumerRef:                # controller-managed active binding
retainedFrom:               # controller-managed previous consumer and cluster identity
  namespace: <namespace>
  name: <name>
  uid: <uid>
  clusterUID: <uid>
reusePolicy: Retain         # user intent; default is Retain
reuseApproval:
  retainedFromUID: <uid>    # must match the current retainedFrom.uid
reuseMode: Adopt            # Adopt or Reprovision
```

`retainedFrom`はMachine削除後も残すため、controller再起動後にfreshなHostとdataを保持するHostを区別できる。`reuseApproval`は現在の`retainedFrom`に対する一回の明示承認として扱い、成功したclaimで消費し、別のMachineをclaimした後や次の削除後へ自動継承しない。validationまたはreconcileは、retainedFromがない状態での`Reusable`指定を将来の削除承認として扱わない。

Hostのallocation eligibilityは`Available`、`Claimed`、`Retained`、`Reusable`を区別する。これはworkflow phaseではなく、Host selectorへ含めてよいかを表す観測である。freshなHostは`spec.consumerRef`と`spec.retainedFrom`がなく、`Retained`の履歴がないため`Available`である。Machine削除後はcontroller-managedな`spec.retainedFrom`へ直前のconsumer UID、namespace、nameを残し、claimを解除しても`Retained`にする。`Retained`は`Available`ではなく、`spec.reusePolicy: Reusable`、一致する`spec.reuseApproval.retainedFromUID`、`spec.reuseMode`、停止・identity・inventoryの安全条件がそろった場合だけ`Reusable`になる。再利用承認が古い`retainedFrom`を指す場合や承認を先に設定した場合は自動allocationしない。

`Reusable`の動作は明確に分ける。`Adopt`は既存Talos installation、cluster identity、desired configurationが対象Machineと一致する場合だけ同じdataを保持してclaimする。`Reprovision`はユーザーが明示的にdata破棄を承認した別lifecycleであり、Talosのreset/installer機構へ委譲してから新しいMachineへclaimする。いずれも通常のselector allocation、Machine update、Machine deletionのfallbackとして実行しない。

`TartHost`の直接削除は安全なforget操作として扱う。Claim中のHostはconsumerが存在する限り削除をBlockし、Retained Hostも`TartHost`へ`tart.cluster.x-k8s.io/forget-approved: "true"` annotationが付くまで削除しない。forget承認後の削除もpower off、Talos reset、disk wipeを実行せず、Tartのinventoryからだけ取り除く。Retained Hostの物理dataを保持したまま管理対象から忘れさせる操作であり、同じHostの再利用承認とは別である。

## `TartClusterSpec`

CAPIのcontrol plane endpointとcluster全体に必要なinfrastructure設定だけを持つ。Host、Machine、Talos disk、CNI、CSI、kube-vipなどのnode固有またはadd-on固有設定は持たない。

TartClusterがfailure domainsをsurfaceする場合、`TartHost.spec.failureDomain`から`TartCluster.status.failureDomains`、CAPI `Machine.spec.failureDomain`、Host allocatorまで同じ値を接続し、対応するMachineを必ず一致するHostへ割り当てる。failure domainをallocationまで接続できない段階では、Statusへ部分的なfailure domainをsurfaceしない。

## `TartMachineSpec`

| field | 用途 | update policy |
| --- | --- | --- |
| `hostRef` | Hostの明示指定 | Identity。claim後は変更不可 |
| `hostSelector` | Hostのdeterministicな選択条件 | claim後の変更はblocked |
| `talosImage` | `version`と`schematicID`からなるdesired image identity | Talos APIによるin-place update |
| `providerID` | Host UIDから決定論的に生成する物理Node identity | Host claim後に`tart://host/<TartHost UID>`として確定し、Node `spec.providerID`と一致させる |

`talosImage`は次のidentityを一つの正本として扱う。

```yaml
version: v1.13.0
schematicID: 0123456789abcdef
```

Image Factoryのschematicに含まれるsystem extension setもこのidentityに含まれる。PXE/ISOなどのboot assetとTalos installer imageには同じschematicを使い、BootstrapConfigへsystem extensionやinstaller imageを別のdesired stateとして持たせない。

Machine role、Kubernetes version、cluster endpoint、PKI、machine-specific identityはsurrounding CAPI resourceとcontroller-managed contextから導出する。ProviderIDはHost allocation後にTartHost UIDから`tart://host/<TartHost UID>`として決定し、Infrastructure ProviderとBootstrap Providerが同じ決定論的な関数で算出する。Host allocationはbootstrap dataやTalos provisioningを待たずにbindingとProviderIDを確立できるが、Talosのboot、install、power操作はCAPI Machineのbootstrap dataが存在するまで開始しない。この分離によりHost identityを使ったAdoptとCAPIのBootstrap Secret生成を循環させない。provider resourceへkernel parameter、partition、Cilium、Longhorn、TopoLVM、kube-vipなどの専用fieldを追加しない。

## `TartBootstrapConfigSpec`

BootstrapConfigのuser-facing SpecはTalos-native configurationの入力だけに限定する。Kubernetes version、machine role、cluster endpoint、providerID、cluster secret bundle、installer imageをSpecへ重複させない。

```text
configPatches: []RawExtension
configSecretRef: optional immutable Secret reference for user-owned raw configuration
```

Kubernetes versionは対応するCAPI `Machine.spec.version`、roleはCAPI `Machine`のcontrol-plane labelとowner、cluster endpointは`Cluster.spec.controlPlaneEndpoint`、Talos imageは`TartMachine.spec.talosImage`、ProviderIDは`TartMachine.spec.providerID`から取得する。すべてのcontrol plane Machineへ通常のTalos `controlplane` configurationを出し、選択した一台へTalos `Bootstrap` RPCを一度だけ実行する。Talosのdeprecatedな`init` machine typeやetcd recoveryと非互換な初期化方法をProviderのAPIへ持ち込まない。

Bootstrap ProviderはClusterごとに一つのcluster secret bundleを参照し、各Machine向けにmachine-specific configurationを合成する。BootstrapConfigごとにTalos secretsをgenerateしてはならない。

`configSecretRef`が参照するSecretは`immutable: true`を必須とする。内容を変更する場合は新しいSecret名を作成して`TartBootstrapConfig.spec.configSecretRef`を更新し、同一Secretのin-place変更でBootstrapConfigのdesired diffを隠してはならない。Bootstrap Providerは初回Secretを作成した後、mutableなconfiguration変更を検知してBootstrap Secretを再生成しない。mutableなTalos configurationの適用はUpdate Extensionだけが担当する。

## `TartControlPlaneSpec`

`version`、`replicas`、`machineTemplate.spec.infrastructureRef`、`machineTemplate.spec.deletion`、provider-specificな`bootstrapConfigTemplate`をCAPI Control Plane contractと整合する形で持つ。machine templateの`infrastructureRef`は`TartMachineTemplate`を参照し、`bootstrapConfigTemplate`は対応する`TartBootstrapConfigTemplate`の入力として使う。削除時の`nodeDrainTimeoutSeconds`、`nodeVolumeDetachTimeoutSeconds`、`nodeDeletionTimeoutSeconds`は`spec.machineTemplate.spec.deletion`へ置き、metadata、deletion timeout、`minReadySeconds`などrolloutを起こさずに伝播するfieldと区別する。Control Plane Providerは各control-plane CAPI `Machine`の作成時に`spec.infrastructureRef`と生成したBootstrap Configの`spec.bootstrap.configRef`を設定し、対応する`TartMachine`と`TartBootstrapConfig`へ一対一で接続する。

概念上のMachine templateは次の形であり、`infrastructureRef`を`spec`直下へ平坦化しない。

```yaml
spec:
  machineTemplate:
    spec:
      infrastructureRef: ...
      deletion:
        nodeDrainTimeoutSeconds: ...
        nodeVolumeDetachTimeoutSeconds: ...
        nodeDeletionTimeoutSeconds: ...
  bootstrapConfigTemplate: ...
```

Control Plane Providerは次を所有する。

- control plane Machineのdesired replica countとlifecycle
- Talos etcdの初回bootstrapとmember safety
- Cluster secret bundleの生成と維持
- `controlPlaneInitialized`、`Available`、`Ready`の観測
- workload clusterの`<cluster>-kubeconfig` Secretの生成と維持
- Kubernetes versionのcluster-wide upgrade sequencing
- 各control-plane Machineの`spec.minReadySeconds`と`UpToDate` Conditionの継続的な管理
- control-plane Machineへのmetadata propagationとNodeへのmetadata同期。metadata変更はrolloutを起こさない
- `status.versions`、`selector`、`replicas`、`readyReplicas`、`availableReplicas`、`upToDateReplicas`とscale subresource

CNI、CSI、kube-vip、observability、application workloadの設定は持たない。Control Plane Providerはcontrol plane endpointのVIPをallocateしない。利用者、IPAM、または別Infrastructure Providerが設定する`Cluster.spec.controlPlaneEndpoint`を正本として利用し、未設定ならその値が設定されるまでreconcileを進めない。

## Secret contract

### Bootstrap Secret

Bootstrap ProviderはCAPI Bootstrap contractに従い、対応する`TartBootstrapConfig`のcontroller OwnerReferenceを持つ決定論的なSecretを作成する。Secretは次の形に固定する。

```yaml
apiVersion: v1
kind: Secret
type: cluster.x-k8s.io/secret
metadata:
  labels:
    cluster.x-k8s.io/cluster-name: <cluster-name>
  ownerReferences:
    - controller: true
data:
  value: <complete Talos machine configuration>
```

`data`は単一の`value` keyだけを持つ。`talosConfig`、`clusterCA`、`token`などへ独自分解しない。初期実装ではSecret名を対応する`TartBootstrapConfig.metadata.name`とし、`status.dataSecretName`へ記録する。生成済みSecretが存在する場合、同一のdesired configurationから再concileしても名前と内容の意味が変わらない。

### Cluster secret bundle

TartControlPlane Providerが、control-plane MachineやBootstrap Secretより先に、Talos/Kubernetesのcluster-level PKIとsecret materialをClusterごとに一度だけ生成する。Bootstrap Providerはread-only consumerであり、bundleを作成・再生成・更新しない。初期化前に限り、Cluster namespaceの`<cluster-name>-talos-secrets`という決定論的な`Opaque` Secretへ格納し、`immutable: true`、Cluster label、`TartCluster`のcontroller OwnerReferenceを設定する。

cluster secret bundleはimmutableな正本とし、Clusterの全control planeとworkerのconfigurationから参照する。`TartCluster`のdeletion finalizerまたは同等の削除ゲートは、全Managed Machineがshutdownとretentionを完了するまでbundleのOwnerをGC可能にしてはならない。初期化後にbundleが欠落した場合は自動再生成せず、既存clusterのidentityを壊さない`Blocked`として報告する。bundleの値をStatus、Event、log、metricsへ出力しない。

### Workload cluster kubeconfig

Control Plane ProviderはCAPI contractに従い、Cluster namespaceへ`<cluster-name>-kubeconfig` Secretを生成・維持する。Secret typeは`cluster.x-k8s.io/secret`、labelは`cluster.x-k8s.io/cluster-name`、dataは単一の`value` keyとする。controller OwnerReferenceは`TartControlPlane`へ設定し、kubeconfigのprivate keyやtokenをStatusやlogへ出力しない。client certificateを使う場合は短い有効期間と更新を設計し、Secretを一度作って放置しない。

## StatusとConditions

Statusはobserved stateとConditionsだけを表し、workflowのprogram counterやoperation logを表さない。`observedGeneration`を更新し、desired stateをStatusへコピーして正本にしない。

| Resource | contract上の主要Status |
| --- | --- |
| `TartHost` | inventory、addresses、reachability、`Claimed`/`Retained`/`Reusable`の観測、Conditions、observedGeneration |
| `TartCluster` | `initialization.provisioned`、control plane endpoint、failure domains、Conditions、observedGeneration |
| `TartMachine` | `initialization.provisioned`、addresses、failure domain、ProviderID、Talos version、Conditions、observedGeneration |
| `TartBootstrapConfig` | `initialization.dataSecretCreated`、`dataSecretName`、configuration digest、Conditions、observedGeneration |
| `TartControlPlane` | `status.versions`、`initialization.controlPlaneInitialized`、replica counts、selector、kubeconfig観測、Conditions、observedGeneration |

`Ready`、`Available`、`Claimed`、`Retained`、`Reusable`、`InfrastructureReady`、`BootstrapReady`、`TalosReachable`、`Provisioned`、`UpToDate`、`Updating`、`Healthy`、`Blocked`は外部から意味を理解できるConditionとして必要なResourceへ設定する。`Pending`、`Writing`、`Verifying`、`BootTrial`、`Step`などの内部phaseは作らない。

`controlPlaneInitialized`は全NodeがReadyであることを意味しない。Talos control planeが起動し、workload Kubernetes API serverがrequestを受け付けられる状態を表す。CNIが未導入でもこのConditionはtrueになり得る。完全なNode readinessは別の`Available`または`Ready`観測で表す。`status.versions`をKubernetes versionの正本として、各entryを古いversionから新しいversionの順に並べる。古い`status.version`は新設しない。

## Ownershipとdeletion

`TartMachine`、`TartBootstrapConfig`、control plane配下のMachineは対応するCAPI resourceとのOwnerReferenceとCAPI labelを設定する。`TartHost`はCAPI Machineより長寿命なのでMachineのOwnerReferenceを持たない。Host bindingの排他性はcontroller-managedな`TartHost.spec.consumerRef`で表し、`TartHost.status.claimedBy`をlockの正本にしない。`spec.retainedFrom`はMachine削除時に残すcontroller-managedな安全記録であり、fresh Hostと過去のdataを保持するHostを再起動後も区別する。Retained recordには直前のconsumer UIDだけでなくcluster UIDを含め、cluster deletion後の再利用制約を観測から復元できるようにする。

削除責務はCAPIの標準lifecycleと分離する。CAPI Machine controllerがdrainとvolume detachを実施し、Control Plane Providerがscale-down時に`pre-terminate.delete.hook.machine.cluster.x-k8s.io/...`でetcd member removalを完了する。`TartMachine`のfinalizerはauthenticated Talos shutdown/quiesce、停止確認、`spec.retainedFrom`の記録、claim解放を担当する。Talos APIに到達できず停止を確認できない場合、既定ではclaimとfinalizerを保持して`Blocked`にする。Cluster全体の削除ではscale-downと異なりetcd quorum維持のmember removalを必須にせず、Control Plane Providerはpre-terminate hookを安全に完了させて削除を進める。cluster secret bundleは全Managed Machineのshutdownとretention完了後にだけGC可能とし、cluster deletion後に残るHostは`Adopt`不可、`Reprovision`専用として扱う。明示的なforce releaseを導入する場合も、通常の削除やupdateから暗黙に実行しない。

Machine、Host、disk、local persistent dataのidentityはUID、reference、stable hardware identity、Talos observed stateで確認する。name、Pod名、Node名、resourceVersionをidentityの正本にしない。

## Update policy

| 区分 | 例 | 取り扱い |
| --- | --- | --- |
| In-place mutable | Talos version、schematic、Talosがreboot付きで受理するconfiguration、CAPI Kubernetes version lifecycle | Talos/CAPIのupdate semanticsへ委譲 |
| Initial-only | installation targetを変えるdisk topology、Host selectorのclaim後変更 | 自動適用せずblocked |
| Destructive | dataを消すreprovisioning、disk wipe、partition変更 | 通常updateから呼び出さない |
| Identity | Host binding、system identity、Machine identity、ProviderID | 通常updateから変更しない |

`CanUpdateMachineSet`、`CanUpdateMachine`、`UpdateMachine`は一体のRuntime Extension contractとして扱う。`CanUpdateMachineSet`はMachineDeploymentのMachineSet差分を、`CanUpdateMachine`は個別Machine差分を判定する。安全に全desired diffをcoverできる場合だけ`Success`と、current objectへ適用すればそのcoverageを表す完全なJSON PatchまたはJSON Merge Patchを返す。危険、未知、または一部しかcoverできない差分はpatchを返さず`Failure`とし、messageへblocked理由を返す。`Success`で部分的なpatchを返してCAPIに残りの差分をimmutable rolloutさせてはならない。`UpdateMachine`は同じMachineへTalos operationを適用し、`Success + retryAfterSeconds > 0`を実行中、`Success + retryAfterSeconds = 0`を完了として返す。

`TartMachine`の通常reconcileと`TartBootstrapConfig`の通常reconcileは、初回provisioning前のinstallation deliveryと観測Status更新だけを担当する。初回provisioning後のmutableなTalos OS/config変更、BootstrapConfigの再生成、Talos upgradeはUpdate Extensionだけが実行する。通常controllerがdesiredとactualの差分を見て同じTalos operationを開始してはならない。CAPIが`Failure`をimmutable rolloutへ変換するversionでは、Tartの対象CRD webhook、MHC policy、rollout profile、Retained gateを合わせてreplacementが開始される構成を受け入れず、deployment prerequisiteとして拒否する。

Control Plane Providerがin-place updateを開始する場合は、まず`CanUpdateMachine`を呼ぶ。`Success`の場合だけresourceVersionを確認しながらCAPI Machine、対応するTartMachine、TartBootstrapConfigのdesired specを更新し、3つすべてへ`in-place-updates.internal.cluster.x-k8s.io/update-in-progress` annotationを設定した後、Machineへ`UpdateMachine` hook pendingを設定する。この遷移は既存annotation、desired spec、hook pending、各resourceのgenerationを観測して再入可能にし、途中で再起動しても二重の更新や部分的なidentity変更を作らない。`Failure`または更新対象外の場合はspecを変更せず、Control Planeを`Blocked`として停止する。

## Runtime Extensionの前提

in-place updateを有効にするmanagement clusterでは、CAPIの`RuntimeSDK=true`と`InPlaceUpdates=true`をfeature gateへ設定する。TartのHTTPS endpointを`ExtensionConfig`へ登録し、server certificate、TLS Secret、必要なCA trustを管理する。in-place update hookへ登録できるextensionが一つに制限されるCAPI versionでは、Tart以外のhookと同時に登録しない。CRDには`cluster.x-k8s.io/v1beta2: v1alpha1`のcontract-version labelを付与する。

Control Plane CRDはCAPI v1beta2 contractに従う`status.versions`、`status.selector`、`status.replicas`、`status.readyReplicas`、`status.availableReplicas`、`status.upToDateReplicas`を持ち、`spec.replicas`、`status.replicas`、`status.selector`を接続する`scale` subresourceを生成する。Conditionは少なくとも`Available`を実装し、`RollingOut`、`ScalingUp`、`ScalingDown`、`MachinesReady`、`MachinesUpToDate`、`EtcdClusterAvailable`、`Deleting`、`Paused`などCAPI標準semanticsに合わせた観測を必要に応じて公開する。control-plane Machineへmetadataをrolloutなしで伝播し、`spec.minReadySeconds`と`UpToDate` Conditionを継続的に管理する。
