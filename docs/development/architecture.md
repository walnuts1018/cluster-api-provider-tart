# アーキテクチャ

この文書は、Tartの設計判断を実装へ落とし込むための構成方針である。TartはTalos Linux専用のCluster API Providerであり、Infrastructure Provider、Bootstrap Provider、Control Plane Providerを一つのcontroller-managerで提供する。

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
|---|---|---|
| Infrastructure | `TartHost`のinventory、Host allocation、power/boot、Talos installation delivery、ProviderID、addresses、infrastructure readiness、Talos OSのinfrastructure側lifecycle | Talos configurationの生成、CNI/CSIなどのadd-on、workloadの配置 |
| Bootstrap | Talos machine configurationの生成、cluster secretsとの結合、machine roleごとのconfiguration、bootstrap data Secret | OS installation、Host power、etcd membership |
| Control Plane | replica、control plane Machine、初回etcd bootstrap、health、Kubernetes lifecycle、quorumを考慮したscaleとupdate | Host inventory、disk writer、Kubernetes add-on |

TalosのOS、disk、volume、machine configuration、upgrade、rollback、Kubernetes runtimeはTalosへ委譲する。TartはTalosの機能を別のdomain modelへ写し替えず、必要なidentity、desired state、観測結果だけをCAPIのresourceへ接続する。

## 実装パッケージの境界

ルート直下のパッケージを、現在複数の具体的な利用箇所がある責務に限定して配置する。`internal`や`pkg`は作成しない。また、webアプリケーション由来の巨大な`domain`、`infrastructure`、`workflow`階層も作らない。

```text
api/v1alpha1             CRDのSpec、Status、Condition、DeepCopy対象の型だけ
controller               Kubernetes watchとreconcileの薄いentrypoint
host                     Host選択、claim、identity、power/boot境界
talos                    Talos API clientと観測結果へのadapter
bootstrap                Talos configuration生成とpatch合成の純粋な判断
controlplane             etcd/control planeの安全性判断とlifecycle policy
boot                     maintenance boot backendの最小interfaceとadapter
extensions               CAPI Runtime Extensionのwire protocolとupdate policy
cmd/controller-manager   manager、scheme、controller、HTTP endpointの組み立て
```

全てのパッケージを最初から作る必要はない。副作用を含む実装は該当するadapterへ置き、controllerはKubernetes resourceを読み、純粋なpolicyを呼び、外部状態をStatusへ反映する。Kubernetes client、Talos client、power、bootのinterfaceは、複数の実装または副作用の隔離が実際に必要な境界にだけ置く。

依存方向は次を基本とする。

```text
cmd/controller-manager
        |
        +--> controller --> api/v1alpha1
        |       |
        |       +--> host / talos / bootstrap / controlplane
        |       +--> extensions
        |
        +--> boot / talosの具体的adapter
```

純粋なpolicy packageからKubernetes clientを参照しない。Talosの具体的なgenerated API型をcontrollerの判断ロジックへ漏らさず、`talos`が観測結果と操作を小さなinterfaceへ変換する。

## Reconcileの原則

ReconcileはResource Statusを手順番号として扱わず、次の観測値から毎回判断する。

```text
Kubernetes desired state
  + TartHost observed inventory / claim
  + Talos API observed version / configuration / health
  + workload cluster observed state
      ↓
next safe action or Condition
```

副作用の前後でcontrollerが停止しても、次回のreconcileでversion、reachability、configuration digest、ProviderID、etcd healthなどを再取得する。同じAPI呼び出しを再試行しても安全でない操作は、事前に観測で完了を確認できるadapterだけから呼び出す。

Kubernetes resourceの作成・Status反映はserver-side applyを基本とし、field ownershipをcontrollerごとに分ける。OwnerReferenceとCAPIのlabelを一貫して設定し、名称やprocess memoryをidentityの正本にしない。

## Provider APIとCAPI contract

TartのAPI group/versionは`infrastructure.cluster.x-k8s.io/v1alpha1`とする。過去のTart APIとの互換性は維持しない。一方、Infrastructure Cluster、Infrastructure Machine、Bootstrap Config、Control PlaneのStatusとreferenceは、実装時点のCluster API v1beta2 provider contractに合わせる。

つまり、API versionの`v1alpha1`はTart独自APIのバージョンであり、CAPI coreの`v1beta2`を意味しない。例えばInfrastructure Machineでは`spec.providerID`、`status.initialization.provisioned`、`status.addresses`、Bootstrap Configでは`status.dataSecretName`と`status.initialization.dataSecretCreated`、Control Planeでは`spec.version`、`spec.replicas`、`status.readyReplicas`などをcontractに従って扱う。

ClusterClassから利用できるよう、template resourceは通常のCAPI template semanticsに従う。Tart固有のinstaller pathやadd-on resourceをCluster topologyへ要求しない。

## 副作用の境界

### Kubernetes API

Resourceの取得、list、watch、server-side apply、Status patch、Eventを担当する。Controller以外のpackageはcontroller-runtime clientを直接利用しない。

### Talos API

maintenance modeのhardware discovery、authenticated APIのversion/health観測、configuration apply、install、upgrade、bootstrapを担当する。Tartはblock deviceへの直接書き込み、partition編集、独自updater、独自rollbackを行わない。

### Host lifecycle

Hostのallocation、claimの競合確認、power on、maintenance bootの要求を担当する。Wake-on-LAN、Redfish、VM API、手動起動はbackendの差であり、TartMachineのidentityやStatusの意味を変えない。

### Runtime Extension

CAPIのin-place update hookを受け、変更をin-placeで安全に適用できるか判定する。安全に扱えない変更をMachine replacementへ委譲することは許可しない。patchの対象とupdate開始の結果はCAPIのhook contractで表現し、Operation CRDは作らない。

## 明示的に作らないもの

TartHostOperation、Workflow engine、Provisioning Plan、独自Provisioning Agent、独自Node Lifecycle Agent、独自OS image format、disk writer、partition DSL、A/B updater、rollback manager、add-on専用APIは作らない。ネットワークbootの具体的なDHCP/TFTP/PXE実装もTartのdomain modelの中心へ置かない。

CRD、RBAC、DeepCopyの生成方法と変更時の確認は[開発ガイド](development.md)と[検証方針](verification.md)を参照する。
