# アーキテクチャ

この文書は、Tartの設計判断を実装へ落とし込むための構成方針である。TartはTalos Linux専用のCluster API Providerであり、Infrastructure Provider、Bootstrap Provider、Control Plane Providerを提供する。

## 責務の全体像

```text
                    Cluster API
                         |
       +-----------------+-----------------+
       |                 |                 |
       v                 v                 v
 Infrastructure      Bootstrap        Control Plane
    Provider          Provider          Provider
       |                 |                 |
       +-----------------+-----------------+
                         |
                    Tart resources
                    /      |      \
                   v       v       v
                Host     Talos   Kubernetes
              lifecycle   API       API
```

各Providerの所有範囲は次のとおりである。

| Provider | 所有する責務 | 所有しない責務 |
| --- | --- | --- |
| Infrastructure | `TartHost`のinventory、Host allocation、power/boot、Talos installation delivery、ProviderID、addresses、infrastructure readiness、Talos OSのinfrastructure側lifecycle | Talos configurationの生成、cluster secretの生成、CNI/CSIなどのadd-on、workloadの配置 |
| Bootstrap | Talos machine configurationの生成、cluster secret bundleの参照、machine-specific configuration、bootstrap data Secret | OS installation、Host power、etcd membership、kubeconfig |
| Control Plane | replica、control plane Machine、初回etcd bootstrap、health、Kubernetes lifecycle、quorumを考慮したscale/update、workload kubeconfig Secret | Host inventory、disk writer、Kubernetes add-on |

TalosのOS、disk、volume、machine configuration、upgrade、rollback、Kubernetes runtimeはTalosへ委譲する。TartはTalosの機能を別のdomain modelへ写し替えず、必要なidentity、desired state、観測結果だけをCAPIのresourceへ接続する。

## API groupとresource

Tart独自APIは`v1alpha1`へリセットし、CAPI coreの`v1beta2` contractへ接続する。CAPIのprovider conventionに合わせ、Infrastructure resourceは`infrastructure.cluster.x-k8s.io/v1alpha1`、Bootstrap resourceは`bootstrap.cluster.x-k8s.io/v1alpha1`、Control Plane resourceは`controlplane.cluster.x-k8s.io/v1alpha1`へ分ける。別groupのreferenceをCAPI coreが扱えるようaggregated RBACを生成する。

`TartHost`はCAPI `Machine`より長寿命のinventoryであり、controller-managedな`spec.consumerRef`でallocation bindingを表す。Machine削除後はHostを`Retained`として保持し、`Reusable`への明示変更なしに自動allocationへ戻さない。

## 実装パッケージの境界

ルート直下のパッケージを、現在複数の具体的な利用箇所がある責務に限定して配置する。`internal`や`pkg`は作成しない。また、webアプリケーション由来の巨大な`domain`、`infrastructure`、`workflow`階層も作らない。

```text
api/infrastructure/v1alpha1  Infrastructure CRDのSpec、Status、Condition、DeepCopy対象の型
api/bootstrap/v1alpha1       Bootstrap CRDのSpec、Status、Condition、DeepCopy対象の型
api/controlplane/v1alpha1    Control Plane CRDのSpec、Status、Condition、DeepCopy対象の型
controller                   Kubernetes watchとreconcileの薄いentrypoint
host                         Host選択、consumerRef claim、identity、power/boot境界
talos                       Talos API clientとobserved stateへのadapter
bootstrap                    Talos configuration生成とpatch合成の判断
controlplane                 etcd/control planeの安全性判断とKubernetes lifecycle policy
boot                         maintenance boot backendの最小interfaceとadapter
extensions                   CAPI Runtime Extensionのwire protocolとupdate policy
cmd/controller-manager       manager、scheme、controller、HTTPS endpointの組み立て
```

全てのパッケージを最初から作る必要はない。副作用を含む実装は該当するadapterへ置き、controllerはKubernetes resourceを読み、純粋なpolicyを呼び、外部状態をStatusへ反映する。interfaceは外部副作用の隔離または実際に複数実装が必要な境界だけに定義する。

依存方向は次を基本とする。

```text
cmd/controller-manager
        |
        +--> controller --> api/*/v1alpha1
        |       |
        |       +--> host / talos / bootstrap / controlplane
        |       +--> extensions
        |
        +--> boot / talosの具体的adapter
```

純粋なpolicy packageからKubernetes clientを参照しない。Talosのgenerated API型をcontrollerの判断ロジックへ漏らさず、`talos`が観測結果と操作を小さな型へ変換する。

## Reconcileの原則

ReconcileはResource Statusを手順番号として扱わず、次の観測値から毎回判断する。

```text
Kubernetes desired state
  + TartHost spec/statusとinventory
  + Talos API observed version/configuration/health
  + workload cluster observed state
      ↓
next safe action or Condition
```

副作用の前後でcontrollerが停止しても、次回のreconcileでversion、reachability、configuration digest、ProviderID、etcd health、Secret、Node状態を再取得する。同じAPI呼び出しを再試行しても安全でない操作は、観測で完了を確認できるadapterだけから呼び出す。

Kubernetes resourceの作成・Status反映はserver-side applyを基本とし、field ownershipをcontrollerごとに分ける。ただし`TartHost.spec.consumerRef`はStatusではなくcontroller-managed desired bindingとして排他性の正本にする。

## CAPI contractとupdate

Infrastructure Cluster、Infrastructure Machine、Bootstrap Config、Control PlaneのStatusとreferenceは、実装時点のCluster API v1beta2 provider contractに合わせる。Infrastructure Machineでは`spec.providerID`、`status.initialization.provisioned`、`status.addresses`、Bootstrap Configでは`status.dataSecretName`と`status.initialization.dataSecretCreated`、Control Planeでは`spec.version`、`spec.replicas`、`status.readyReplicas`、workload kubeconfig Secretをcontractに従って扱う。

Runtime Extensionの`CanUpdateMachine`は安全なin-place差分だけをtrueとし、`UpdateMachine`は同じMachineへTalos operationを適用する。CAPIがhook未対応差分をimmutable rolloutへfallbackし得るため、hookだけでreplacementを禁止したとみなさない。`TartMachine`のblocked判定、Retained Host gate、`maxSurge: 0`/`maxUnavailable: 1`のrollout profile、MHCのskip-remediation policyを組み合わせる。

`controlPlaneInitialized`は全Node ReadyやCNI導入を意味せず、Kubernetes API serverがrequestを受け付けられる状態を表す。CNI、CSI、kube-vip、observability、application workloadの配布はClusterResourceSet、Addon Provider、GitOpsなどへ委譲する。

## Runtime Extension deployment prerequisites

in-place updateを有効にするmanagement clusterではCAPIの`RuntimeSDK=true`と`InPlaceUpdates=true` feature gateを設定する。TartのHTTPS endpointを`ExtensionConfig`へ登録し、TLS Secret、server certificate、必要なCA trustを管理する。現行CAPIではin-place update hookへ登録できるextensionは1つなので、他extensionとの競合がないことをdeployment前に確認する。

## 副作用の境界

### Kubernetes API

Resourceの取得、list、watch、server-side apply、Status patch、Event、Secretを担当する。純粋なpolicy packageはcontroller-runtime clientを直接利用しない。

### Talos API

maintenance modeのhardware discovery、authenticated APIのversion/health観測、configuration apply、install、upgrade、shutdown、bootstrapを担当する。Tartはblock deviceへの直接書き込み、partition編集、独自updater、独自rollbackを行わない。

### Host lifecycle

Hostのallocation、`consumerRef`の競合確認、power on、maintenance boot、shutdown確認を担当する。Wake-on-LAN、Redfish、VM API、手動起動はbackendの差であり、TartMachineのidentityやStatusの意味を変えない。

### Runtime Extension

CAPIのin-place update hookを受け、変更をin-placeで安全に適用できるか判定する。安全に扱えない変更をMachine replacementへ委譲することは許可しない。patchの対象とupdate開始の結果はCAPIのhook contractで表現し、Operation CRDは作らない。

## 明示的に作らないもの

TartHostOperation、Workflow engine、Provisioning Plan、独自Provisioning Agent、独自Node Lifecycle Agent、独自OS image format、disk writer、partition DSL、A/B updater、rollback manager、add-on専用APIは作らない。ネットワークbootの具体的なDHCP/TFTP/PXE実装もTartのdomain modelの中心へ置かない。

CRD、RBAC、DeepCopyの生成方法と変更時の確認は[開発ガイド](development.md)と[検証方針](verification.md)を参照する。
