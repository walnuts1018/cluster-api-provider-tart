# API contract

TartのAPI schemaとCluster API contractの対応を定義する。Tart独自APIは`v1alpha1`へリセットし、CAPI coreの現行`v1beta2` contractへ接続する。過去のTart `v1beta1`とのconversionや互換層は作らない。

## API group

CAPIのproviderごとの責務に合わせてAPI groupを分ける。別groupのprovider resourceをCAPI coreが参照できるよう、生成するCRD、RBAC、aggregated roleに必要な権限を含める。

| Resource | Group/version |
| --- | --- |
| `TartHost`、`TartCluster`、`TartClusterTemplate`、`TartMachine`、`TartMachineTemplate` | `infrastructure.cluster.x-k8s.io/v1alpha1` |
| `TartBootstrapConfig`、`TartBootstrapConfigTemplate` | `bootstrap.cluster.x-k8s.io/v1alpha1` |
| `TartControlPlane`、`TartControlPlaneTemplate` | `controlplane.cluster.x-k8s.io/v1alpha1` |

ここでの`v1alpha1`はTart独自APIのversionであり、CAPI coreの`v1beta2`を意味しない。

## Resource一覧

`TartHost`はCAPI外の長寿命Host inventoryである。`TartCluster`、`TartMachine`、`TartBootstrapConfig`、`TartControlPlane`はそれぞれCAPIのInfrastructure Cluster、Infrastructure Machine、Bootstrap Config、Control Plane contractへ接続する。Template resourceは通常のCAPI template semanticsに従う。

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

`spec.consumerRef`はcontrollerが管理するbindingであり、ユーザーが任意のMachineへ向ける通常設定ではない。namespace、name、UIDを含む参照をserver-side applyで排他的に更新し、`TartHost.status`をallocation lockの正本にしない。Statusには`Claimed` Conditionと観測されたconsumerを必要な範囲で保持する。

`spec.reusePolicy`はユーザーが明示するallocation policyで、既定値は`Retain`とする。`Reusable`への変更だけが、保持されたHostを再びselector候補に含める明示的な許可になる。controllerはこのSpecを自動変更しない。

Hostのallocation eligibilityは`Available`、`Claimed`、`Retained`、`Reusable`を区別する。これはworkflow phaseではなく、Host selectorへ含めてよいかを表す観測である。Machine削除後の既定値は`Retained`であり、`Retained`は`Available`ではない。`spec.reusePolicy: Reusable`が設定され、かつ停止・identity・inventoryの安全条件を再確認できるまで自動allocationの候補に戻さない。destructiveなreprovision/cleanと既存installationのadoptは初期実装の通常allocationに含めず、別の明示的なlifecycleとして扱う。

## `TartClusterSpec`

CAPIのcontrol plane endpointとcluster全体に必要なinfrastructure設定だけを持つ。Host、Machine、Talos disk、CNI、CSI、kube-vipなどのnode固有またはadd-on固有設定は持たない。

TartClusterがfailure domainsをsurfaceする場合、`TartHost.spec.failureDomain`から`TartCluster.status.failureDomains`、CAPI `Machine.spec.failureDomain`、Host allocatorまで同じ値を接続し、対応するMachineを必ず一致するHostへ割り当てる。failure domainをallocationまで接続できない段階では、Statusへ部分的なfailure domainをsurfaceしない。

## `TartMachineSpec`

| field | 用途 | update policy |
| --- | --- | --- |
| `hostRef` | Hostの明示指定 | Identity。claim後は変更不可 |
| `hostSelector` | Hostのdeterministicな選択条件 | claim後の変更はblocked |
| `talosImage` | `version`と`schematicID`からなるdesired image identity | Talos APIによるin-place update |
| `providerID` | Tartが生成するNode identity | claim後に固定し、Node `spec.providerID`と一致させる |

`talosImage`は次のidentityを一つの正本として扱う。

```yaml
version: v1.13.0
schematicID: 0123456789abcdef
```

Image Factoryのschematicに含まれるsystem extension setもこのidentityに含まれる。PXE/ISOなどのboot assetとTalos installer imageには同じschematicを使い、BootstrapConfigへsystem extensionやinstaller imageを別のdesired stateとして持たせない。

Machine role、Kubernetes version、cluster endpoint、PKI、machine-specific identityはsurrounding CAPI resourceとcontroller-managed contextから導出する。provider resourceへkernel parameter、partition、Cilium、Longhorn、TopoLVM、kube-vipなどの専用fieldを追加しない。

## `TartBootstrapConfigSpec`

BootstrapConfigのuser-facing SpecはTalos-native configurationの入力だけに限定する。Kubernetes version、machine role、cluster endpoint、providerID、cluster secret bundle、installer imageをSpecへ重複させない。

```text
configPatches: []RawExtension
configSecretRef: optional Secret reference for user-owned raw configuration
```

Kubernetes versionは対応するCAPI `Machine.spec.version`、roleはCAPI `Machine`のcontrol-plane labelとowner、cluster endpointは`TartCluster`、Talos imageは`TartMachine.spec.talosImage`、ProviderIDは`TartMachine.spec.providerID`から取得する。最初のcontrol planeへTalos `init`相当のconfigurationを出すかどうかは、Control Plane Providerのobserved initializationから導出し、BootstrapConfigのrole fieldへ保存しない。

Bootstrap ProviderはClusterごとに一つのcluster secret bundleを参照し、各Machine向けにmachine-specific configurationを合成する。BootstrapConfigごとにTalos secretsをgenerateしてはならない。

## `TartControlPlaneSpec`

`version`、`replicas`、`machineTemplate.infrastructureRef`、machine template内のBootstrapConfig template referenceをCAPI Control Plane contractと整合する形で持つ。`machineTemplate.infrastructureRef`は`TartMachineTemplate`を参照し、BootstrapConfig template referenceは対応する`TartBootstrapConfigTemplate`を参照する。Control Plane Providerは各control-plane CAPI `Machine`の作成時にこの2つのreferenceを設定し、対応する`TartMachine`と`TartBootstrapConfig`へ一対一で接続する。

Control Plane Providerは次を所有する。

- control plane Machineのdesired replica countとlifecycle
- Talos etcdの初回bootstrapとmember safety
- `controlPlaneInitialized`、`Available`、`Ready`の観測
- workload clusterの`<cluster>-kubeconfig` Secretの生成と維持
- Kubernetes versionのcluster-wide upgrade sequencing

CNI、CSI、kube-vip、observability、application workloadの設定は持たない。

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

Talos/Kubernetesのcluster-level PKIとsecret materialはClusterごとに一度だけ生成する。BootstrapConfigが個別にbundleを生成してはならない。初期化前に限り、Cluster namespaceの`<cluster-name>-talos-secrets`という決定論的な`Opaque` Secretへ格納し、`immutable: true`、Cluster label、`TartCluster`のcontroller OwnerReferenceを設定する。

cluster secret bundleはimmutableな正本とし、Clusterの全control planeとworkerのconfigurationから参照する。初期化後にbundleが欠落した場合は自動再生成せず、既存clusterのidentityを壊さない`Blocked`として報告する。bundleの値をStatus、Event、log、metricsへ出力しない。

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
| `TartControlPlane` | version、`initialization.controlPlaneInitialized`、replica counts、selector、kubeconfig観測、Conditions、observedGeneration |

`Ready`、`Available`、`Claimed`、`Retained`、`Reusable`、`InfrastructureReady`、`BootstrapReady`、`TalosReachable`、`Provisioned`、`UpToDate`、`Updating`、`Healthy`、`Blocked`は外部から意味を理解できるConditionとして必要なResourceへ設定する。`Pending`、`Writing`、`Verifying`、`BootTrial`、`Step`などの内部phaseは作らない。

`controlPlaneInitialized`は全NodeがReadyであることを意味しない。Talos control planeが起動し、workload Kubernetes API serverがrequestを受け付けられる状態を表す。CNIが未導入でもこのConditionはtrueになり得る。完全なNode readinessは別の`Available`または`Ready`観測で表す。

## Ownershipとdeletion

`TartMachine`、`TartBootstrapConfig`、control plane配下のMachineは対応するCAPI resourceとのOwnerReferenceとCAPI labelを設定する。`TartHost`はCAPI Machineより長寿命なのでMachineのOwnerReferenceを持たない。Host bindingの排他性はcontroller-managedな`TartHost.spec.consumerRef`で表し、`TartHost.status.claimedBy`をlockの正本にしない。

`TartMachine`のfinalizerは、削除時のdrain、control plane member detach、Talos shutdown、停止確認、claim解放を完了するために使う。Talos APIに到達できず停止を確認できない場合、既定ではclaimを解放せず`Blocked`にする。明示的なforce releaseを導入する場合も、通常の削除やupdateから暗黙に実行しない。

Machine、Host、disk、local persistent dataのidentityはUID、reference、stable hardware identity、Talos observed stateで確認する。name、Pod名、Node名、resourceVersionをidentityの正本にしない。

## Update policy

| 区分 | 例 | 取り扱い |
| --- | --- | --- |
| In-place mutable | Talos version、schematic、Talosがreboot付きで受理するconfiguration、CAPI Kubernetes version lifecycle | Talos/CAPIのupdate semanticsへ委譲 |
| Initial-only | installation targetを変えるdisk topology、Host selectorのclaim後変更 | 自動適用せずblocked |
| Destructive | dataを消すreprovisioning、disk wipe、partition変更 | 通常updateから呼び出さない |
| Identity | Host binding、system identity、Machine identity、ProviderID | 通常updateから変更しない |

`CanUpdateMachine`で扱えるのは安全なin-place差分だけである。CAPIがhook未対応差分をimmutable rolloutへfallbackし得るため、`CanUpdateMachine=false`だけで破壊的replacementをTartが安全に禁止できるとはみなさない。保護対象の差分は`TartMachine`を`Blocked`にし、Hostを`Retained`ゲートで自動再利用不可に保つ。判断不能な差分は安全に更新できない差分として扱う。
