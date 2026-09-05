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
| Infrastructure | `TartHost`のinventory、Host allocation、power/boot、Talos installation delivery、ProviderID、addresses、infrastructure readiness、初回provisioningのTalos delivery | Talos configurationの生成、cluster secretの生成、初回provisioning後のmutableなTalos OS/config update、CNI/CSIなどのadd-on、workloadの配置 |
| Bootstrap | Talos machine configurationの生成、cluster secret bundleのread-only参照、machine-specific configuration、bootstrap data Secret | cluster secret bundleの生成、OS installation、Host power、etcd membership、kubeconfig |
| Control Plane | replica、control plane Machine、cluster secret bundle、初回etcd bootstrap、health、Kubernetes lifecycle、quorumを考慮したscale/update、workload kubeconfig Secret | Host inventory、disk writer、Kubernetes add-on |

TalosのOS、disk、volume、machine configuration、upgrade、rollback、Kubernetes runtimeはTalosへ委譲する。TartはTalosの機能を別のdomain modelへ写し替えず、必要なidentity、desired state、観測結果だけをCAPIのresourceへ接続する。

## API groupとresource

Tart独自APIは`v1alpha1`へリセットし、CAPI coreの`v1beta2` contractへ接続する。CAPIのprovider conventionに合わせ、Infrastructure resourceは`infrastructure.cluster.x-k8s.io/v1alpha1`、Bootstrap resourceは`bootstrap.cluster.x-k8s.io/v1alpha1`、Control Plane resourceは`controlplane.cluster.x-k8s.io/v1alpha1`へ分ける。contractへ参加するCRDには`cluster.x-k8s.io/v1beta2: v1alpha1` labelを付け、別groupのreferenceをCAPI coreが扱えるようaggregated RBACを生成する。

`TartHost`はCAPI `Machine`より長寿命のinventoryであり、management cluster全体で一意なcluster-scoped Resourceとする。Kubernetes metadata UIDとは独立したimmutableな`spec.id`を持ち、management clusterのバックアップからobjectを再作成しても同じ物理Host identityを復元できるようにする。`TartCluster`もCAPI `Cluster.metadata.uid`とは独立したimmutableな`spec.id`を持ち、workload clusterの永続identityを表す。同名Clusterを再作成した場合は新しいIDを割り当て、古いbundle、Retained Host、consumer bindingを再関連付けしない。controller-managedな`spec.consumerRef`でallocation bindingを表し、claimはresourceVersion付きUpdateまたはJSON Patchの`test`によるatomic CASで確立する。Machine削除時はcontroller-managedな`spec.retainedFrom`へ直前のconsumer UIDとcluster IDを記録する。`Retained` Hostは現在のretained UIDに一致する明示的な`Adopt`または`Reprovision`承認なしに自動allocationへ戻さない。`Reusable`はwipeの同義語ではない。

ProviderIDはHost allocation後の`TartHost.spec.id`から`tart://host/<TartHost.spec.id>`として決定する。Infrastructure Providerはbootstrap dataを待たずにHostのconsumerRefをCASでclaimし、この値を`TartMachine.spec.providerID`とCAPI InfraMachineへ設定できる。Enrollment/Discoveryのsecret-free maintenance bootもbootstrap dataを待たずに実行できるが、Talosのconfiguration apply、install、provisioning用power操作はBootstrap Secretが利用可能になるまで開始しない。Bootstrap Providerは同じHost-based ProviderIDをTalos kubeletへ注入する。この責務分離によりhardware discoveryとTalos provisioningの循環を作らない。

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

Kubernetes resourceの作成・Status反映はserver-side applyを基本とし、field ownershipをcontrollerごとに分ける。ただし`TartHost.spec.consumerRef`はStatusではなくcontroller-managed desired bindingとして排他性の正本にし、SSAではなくresourceVersion付きUpdateまたはJSON Patchの`test`によるatomic CASで更新する。

## CAPI contractとupdate

Infrastructure Cluster、Infrastructure Machine、Bootstrap Config、Control PlaneのStatusとreferenceは、実装時点のCluster API v1beta2 provider contractに合わせる。Infrastructure Machineでは`spec.providerID`、`status.initialization.provisioned`、`status.addresses`、Bootstrap Configでは`status.dataSecretName`と`status.initialization.dataSecretCreated`、Control Planeでは`spec.version`、`spec.replicas`、`spec.machineTemplate.spec.infrastructureRef`、`spec.machineTemplate.spec.deletion`、provider-specificな`spec.bootstrapConfigTemplate`、`status.versions`、`status.readyReplicas`、`status.availableReplicas`、`status.upToDateReplicas`、`status.selector`、scale subresource、workload kubeconfig Secretをcontractに従って扱う。`status.version`は新設しない。

Runtime Extensionの`CanUpdateMachineSet`、`CanUpdateMachine`は安全に全desired diffをcoverできる場合だけ`Success`と完全なpatchを返し、危険、未知、または一部しかcoverできない差分はpatchなしの`Failure`として止める。Tartではこの`Failure`をupdateのvetoとして扱い、CAPI minorごとにunsafe diffでMachineSet、Machine、TartHost claimが一つも作成されないことをE2Eで確認する。`UpdateMachine`だけが同じMachineへTalos operationを適用する。通常のInfrastructure/Bootstrap reconcileは初回provisioning後のmutable diffを見てもoperationやBootstrap Secret再生成を開始しない。CAPIがhook未対応差分をimmutable rolloutへfallbackし得るversionでは、Tartのsafe gate、Retained Host gate、Workerの`maxSurge: 0`/`maxUnavailable: 1`、Control Planeの一台ずつの更新、MHCのskip-remediation policyを組み合わせ、replacementへ進む構成をdeployment prerequisiteとして拒否する。
MHCのdelete-and-recreate remediationを抑止する場合は、MachineDeploymentのMachine templateまたは`TartControlPlane`のMachine templateへ`cluster.x-k8s.io/skip-remediation: "true"`をMachine生成前から設定する。Machine作成後の後追いannotationだけを安全性の根拠にしない。

`CanUpdate*`ではSecret参照名の変更だけから安全性を推測しない。old/new双方のimmutable Secretを解決し、effective Talos configurationをrenderしてsemantic diff全体を分類する。missing、unreadable、generation不明は`unknown`としてpatchなしの`Failure`にする。Tart v1alpha1では自動replacementやguided reprovisionのopt-inを提供せず、unsafe diffは安全停止する。利用者のMachine削除はCAPIの通常replacement semanticsを発生させ得て、別のAvailable Hostをclaimする可能性がある。Retained Hostの`Reprovision`承認はdata破棄だけを許可し、Machine削除や同じHostへの再割り当てを開始しない。

node-disruptive updateは、まずcordon/drainを試す。drainが成功すればupdateを開始する。drain失敗がavailability、PDB、capacityだけの理由で`TartCluster.spec.updatePolicy.allowDowntime: true`が明示されている場合はgraceful shutdown/rebootを許可し、未指定または`false`ならavailability理由でも安全停止する。destructive disk change、identity mismatch、Host mismatch、unsafe etcd membership change、quorum violationは緩和しない。

`controlPlaneInitialized`は全Node ReadyやCNI導入を意味せず、Kubernetes API serverがrequestを受け付けられる状態を表す。CNI、CSI、kube-vip、observability、application workloadの配布はClusterResourceSet、Addon Provider、GitOpsなどへ委譲する。

Condition typeはResourceごとに固定する。`TartHost`は`Ready`、`InventoryReady`、`TalosReachable`、`Claimed`、`Retained`、`Reusable`、`TartCluster`は`Ready`、`TartMachine`は`Ready`、`TalosReachable`、`Provisioned`、`UpToDate`、`TartBootstrapConfig`は`Ready`、`TartControlPlane`は`Ready`、`Available`、`UpToDate`、`RollingOut`、`ScalingUp`、`ScalingDown`、`MachinesReady`、`MachinesUpToDate`、`EtcdClusterAvailable`、`Deleting`、`Paused`を使う。CAPIの`InfrastructureReady`と`BootstrapReady`はCAPI Machine側へsurfaceされる標準Conditionであり、provider Resourceへ汎用的に追加しない。安全停止は`Ready=False`または`Available=False`と`Reason=UnsafeUpdate`、`IdentityConflict`、`SecretBundleUnavailable`、`RolledBack`などで表し、`Blocked`を汎用Condition typeにしない。

cluster secret bundleはClusterごとに一度だけという意味を「一つのSecretを永遠にimmutable」と解釈しない。`TartCluster.spec.id`を含むgeneration単位でimmutableなSecretを作成し、active generationを永続的な参照で切り替える。CA rotationではgeneration Nを基にrotation対象のCAだけを更新した完全なgeneration N+1を`Pending`として先に永続化する。その後、Talos公式のaccepted CA追加、issuing CA切替、certificate refresh、旧CA削除のsemanticsをmachine configuration/APIでreconcileする。自動`rotate-ca`を完了後のmaterial回収として扱わず、Pending bundleとobserved stateから再開する。rotation対象外のetcd CA、service account keyなどは変更しない。正常完了を観測してから新generationをactiveに確定し、rotation中は新しいMachineのprovisioningとAdoptを開始しない。Cluster存続中は過去generationをGCせず、削除時にDR保持方針を確認した後だけGCを許可する。

configuration digestはTalosが解釈したeffective machine configurationを正規化し、secret-bearing valueをredaction markerへ置換したsemantic representationへSHA-256を適用する。raw YAMLのfield order、defaulting、serialization差分をdriftとせず、`upgrade-k8s`などが管理するversion-managed fieldはgeneric configuration driftから分離する。更新安全性はStatus digestではなく、old/new Secretを解決したsemantic diffで判定する。secret値を含む内部比較結果をStatus、Event、log、metricsへ出力しない。

management clusterのDRでは`TartHost.spec.id`、`TartCluster.spec.id`、Host identity、consumerRef、retainedFrom、CAPI Machine/provider Resource、全secret bundle generation、power/boot設定を同じ整合点からバックアップする。objectのmetadata UIDが復元で変わってもHost identity、Cluster identity、ProviderIDは変えず、bundleの世代が不明なら既存installationのAdoptを許可しない。`clusterctl move`でこの復元契約を代用しない。

## Runtime Extension deployment prerequisites

in-place updateを有効にするmanagement clusterではCAPIの`RuntimeSDK=true`と`InPlaceUpdates=true` feature gateを設定する。TartのHTTPS endpointを`ExtensionConfig`へ登録し、TLS Secret、server certificate、必要なCA trustを管理する。現行CAPIではin-place update hookへ登録できるextensionは1つなので、他extensionとの競合がないことをdeployment前に確認する。

## 副作用の境界

### Kubernetes API

Resourceの取得、list、watch、server-side apply、Status patch、Event、Secretを担当する。純粋なpolicy packageはcontroller-runtime clientを直接利用しない。

### Talos API

maintenance modeのhardware discovery、authenticated APIのversion/health観測、configuration apply、install、upgrade、shutdown、bootstrapを担当する。Tartはblock deviceへの直接書き込み、partition編集、独自updater、独自rollbackを行わない。

### Host lifecycle

Hostのallocation、`consumerRef`のatomic CAS、Enrollment/Discoveryのmaintenance boot、power on、shutdown確認を担当する。Wake-on-LAN、Redfish、VM API、手動起動はbackendの差であり、TartMachineのidentityやStatusの意味を変えない。DiscoveryはBootstrap Secretを待たず、configuration applyとinstallだけがBootstrap Secretを待つ。WoL-onlyではTalos `Shutdown` RPCの受理後にendpointが消失したことを停止確認とし、物理電源OFFの証明とは区別する。自動Reprovisionを提供するbackendはinstalled OSからmaintenance environmentへ戻せるboot strategyを持つ。

### Runtime Extension

CAPIのin-place update hookを受け、`CanUpdateMachineSet`、`CanUpdateMachine`、`UpdateMachine`で変更をin-placeに安全に適用できるか判定する。`CanUpdate*`の`Success`はdesired diff全体をpatchでcoverできる場合だけ返し、unsafe/unknown/partial diffは`Failure`として停止する。TartではこのFailureをvetoとして扱い、CAPI minorごとのunsafe diff E2Eをrelease契約に含める。Update Extension以外の通常controllerは初回provisioning後のmutableなTalos operationを実行しない。Control Plane Providerがin-place transitionを開始するときは、`CanUpdateMachine`成功後にMachine、InfraMachine、BootstrapConfigをannotation付きで更新し、Machineの`UpdateMachine` hook pendingへ引き継ぐ。node-disruptive operationの前にはTalosの安全なdrainまたはworkload cluster側のcordon/drainを試す。drain失敗がavailability、PDB、capacityだけで`TartCluster.spec.updatePolicy.allowDowntime: true`が明示されていればgraceful shutdown/rebootを許可し、未指定または`false`ならavailability理由でも安全停止する。destructive disk change、identity mismatch、Host mismatch、unsafe etcd membership change、quorum violationは緩和しない。Talos rollbackはdesired Specを戻さず`Failure`、`Reason=RolledBack`とし、Control Planeの次Machineへの更新を停止する。Operation CRDは作らない。

## 明示的に作らないもの

TartHostOperation、Workflow engine、Provisioning Plan、独自Provisioning Agent、独自Node Lifecycle Agent、独自OS image format、disk writer、partition DSL、A/B updater、rollback manager、add-on専用APIは作らない。ネットワークbootの具体的なDHCP/TFTP/PXE実装もTartのdomain modelの中心へ置かない。

CRD、RBAC、DeepCopyの生成方法と変更時の確認は[開発ガイド](development.md)と[検証方針](verification.md)を参照する。
