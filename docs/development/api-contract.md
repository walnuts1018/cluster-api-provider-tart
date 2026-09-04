# API contract

TartのAPI schemaとCluster API contractの対応を定義する。TartのAPI versionは`infrastructure.cluster.x-k8s.io/v1alpha1`とし、CAPI coreの現行`cluster.x-k8s.io/v1beta2` contractを実装する。過去のTart `v1beta1`とのconversionや互換層は作らない。

## Resource一覧

```text
TartHost
TartCluster
TartMachine
TartMachineTemplate
TartBootstrapConfig
TartBootstrapConfigTemplate
TartControlPlane
TartControlPlaneTemplate
```

`TartHost`はCluster API外の長寿命Host inventoryである。その他のResourceはCAPIのCluster、Machine、Bootstrap、Control Plane contractへ接続する。CAPIが所有するCluster topology、Machine version、rollout policyをTartのSpecへ重複して持たせない。

## Spec

### `TartHostSpec`

初期登録に必要なfieldはHost identityとpower/boot capabilityに限定する。MAC addressは主要なenrollment情報とし、system UUID、NIC名、disk UUID、Linux device path、Talos endpointの事前入力を必須にしない。architectureやlabelはallocation条件として指定可能にするが、Talosから取得できるinventoryをユーザーのdesired hardware stateとして複製しない。

### `TartClusterSpec`

CAPIのcontrol plane endpointと、cluster全体に必要なinfrastructure設定だけを持つ。Host、Machine、Talos disk、CNI、CSI、kube-vipなどのnode固有またはadd-on固有設定は持たない。

### `TartMachineSpec`

| field | 用途 | update policy |
|---|---|---|
| `hostRef` | Hostの明示指定 | Identity。claim後は変更不可 |
| `hostSelector` | Hostのdeterministicな選択条件 | claim後の変更はblocked |
| `talosImage` | desired Talos installer/image identity | Talos APIによるin-place update |

Machine role、Kubernetes version、machine configurationはCAPI Machineと`TartBootstrapConfig`から得る。provider resourceへkernel parameter、partition、Cilium、Longhorn、TopoLVM、kube-vipなどの専用fieldを追加しない。

### Template

`TartMachineTemplate`、`TartBootstrapConfigTemplate`、`TartControlPlaneTemplate`は、個々のResourceを生成するためのSpec templateだけを保持する。templateは既存Hostのclaim、MachineのStatus、Talos actual stateを所有しない。ClusterClassから通常のCAPI referenceとして利用できる形にする。

### `TartBootstrapConfigSpec`

Talosの表現力を落とさないため、ユーザー設定はraw configuration patchとして受け取る。

```text
clusterName
machineType: init | controlplane | worker
talosVersion
kubernetesVersion
configPatches: []RawExtension
clusterSecretsRef: Secret reference
```

cluster Secretを省略した場合はBootstrap Providerがcluster単位のSecretを生成し、後続Machineで再利用する。Talos machineryのsecrets bundle、cluster endpoint、machine role、installer image、version contractを使用し、最後にraw patchをTalos patcherへ渡す。TartがTalos configurationのsubsetを独自schemaとして再定義しない。

### `TartControlPlaneSpec`

`version`、`replicas`、Infrastructure reference、Bootstrap Config template referenceをCAPI Control Plane contractと整合する形で持つ。CNI、CSI、kube-vip、observability、application workloadの設定は持たない。Control Plane Providerが作成するCAPI Machineは、通常のCluster、Control Plane、MachineのOwnerReferenceとlabelを持つ。

## Status

Statusはobserved stateとConditionsだけを表し、workflowのprogram counterやoperation logを表さない。

| Resource | contract上の主要Status |
|---|---|
| `TartHost` | inventory、claim、addresses、reachability、Conditions、observedGeneration |
| `TartCluster` | `initialization.provisioned`、failure domains、Conditions、observedGeneration |
| `TartMachine` | `initialization.provisioned`、addresses、failure domain、ProviderIDの観測、Talos version、Conditions、observedGeneration |
| `TartBootstrapConfig` | `initialization.dataSecretCreated`、`dataSecretName`、configuration digest、Conditions、observedGeneration |
| `TartControlPlane` | version、`initialization.controlPlaneInitialized`、replica counts、selector、Conditions、observedGeneration |

`Ready`、`Available`、`Claimed`、`InfrastructureReady`、`BootstrapReady`、`TalosReachable`、`Provisioned`、`UpToDate`、`Updating`、`Healthy`、`Blocked`は外部から意味を理解できるConditionとして必要なResourceへ設定する。`Pending`、`Writing`、`Verifying`、`BootTrial`、`Step`などの内部phaseは作らない。

## Ownershipとdeletion

`TartMachine`、`TartBootstrapConfig`、control plane配下のMachineは対応するCAPI resourceとのOwnerReferenceを設定する。`TartHost`はCAPI Machineより長寿命なのでMachineのOwnerReferenceを持たない。`TartMachine`のfinalizerを使う場合も、削除時にHost claimを解除するためだけに使う。

Machine、Host、disk、local persistent dataのidentityはUID、reference、stable hardware identity、Talos observed stateで確認する。nameの規則、Pod名、Node名、resourceVersionをidentityの正本にしない。

## Update policy

| 区分 | 例 | 取り扱い |
|---|---|---|
| In-place mutable | Talos image、Talosがreboot付きで受理するconfiguration、Kubernetes versionのlifecycle | Talos/CAPIのupdate semanticsへ委譲 |
| Initial-only | installation targetを変えるdisk topology、Host selectorのclaim後変更 | 自動適用せずblocked |
| Destructive | dataを消すreprovisioning、disk wipe、partition変更 | 通常updateから呼び出さない |
| Identity | Host binding、system identity、Machine identity | 通常updateから変更しない |

判断不能な差分は安全に更新できない差分として扱う。`CanUpdateMachine`がfalseまたはblockedを返す場合でも、TartがMachine replacementを許可したことにはしない。
