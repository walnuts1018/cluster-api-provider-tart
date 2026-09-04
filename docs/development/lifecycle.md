# Machine lifecycle

Tartのlifecycleは、Kubernetes resourceのdesired stateと、Host、Talos、workload clusterのobserved stateの比較から毎回再計算する。長時間処理のstep、Operation CRD、process memoryを正本にしない。

## Fresh machine

```text
CAPI Cluster / Machine
        ↓
TartCluster / TartMachine / TartBootstrapConfig
        ↓
Hostを明示指定またはdeterministicに選択
        ↓
Host claimとpower/boot要求
        ↓
maintenance Talosからidentityとhardware inventoryを観測
        ↓
Bootstrap Secretのmachine configurationをTalos APIへapply
        ↓
Talos installer、reboot、authenticated API recovery
        ↓
Talos version、health、ProviderID、addressを観測
        ↓
InfrastructureReady / BootstrapReady / Cluster Ready
```

Tartはimageをblock deviceへ直接書き込まず、partition tableを編集せず、OS installerの代わりを実装しない。installation target、volume、encryption、system extension、kernel module、mount、extra manifestはTalos-native configurationとして指定する。

## Host allocation

明示的な`hostRef`を最優先し、未指定ならarchitecture、label、hardware capability、availabilityを満たすHostをdeterministicに選択する。claim前後にHost UIDとresourceVersionを確認し、別MachineがclaimしたHostを上書きしない。競合時は別Hostの選択または再試行を行う。

claimは`TartHost.status`に観測として保存し、MachineのStatusには対応するHost referenceを保存する。claimの存在だけでTalos installation完了とはみなさず、endpoint、identity、inventory、authenticated API、healthを観測する。

## Control Plane

初回bootstrapは、新規clusterの最初のcontrol plane Machineを選び、Talos etcd bootstrapを未初期化の場合だけ呼び出す。API call直後にStatusを完了へ変更せず、etcd membership、workload Kubernetes API、control plane healthを観測して初期化済みと判断する。controller再起動後もこの観測を先に行い、bootstrap済みclusterへ再初期化を送らない。

scale upは既存control planeがhealthyであることを確認し、新MachineのTalos configuration、reachability、healthを確認してからready countへ反映する。scale downは対象memberを安全にremoveでき、quorumを維持できるとTalos/etcdの観測で判断してからMachine削除へ進む。判定不能または危険な場合は削除せず、`Blocked`または`UnsafeControlPlaneOperation`を設定する。

## Update

Talos OS update、Talos machine configuration update、Kubernetes update、Machine replacementを別のlifecycleとして扱う。

### Talos OS

desired imageとTalos actual versionを比較し、差分がある場合だけTalos upgrade APIを呼ぶ。reboot中はtransient stateとして待ち、reboot後にTalos API、version、healthを再取得する。intermediate version、upgrade failure、rollbackはTalosが提供するsemanticsへ委譲する。

### Machine configuration

configuration digestが変わった場合、Talos APIが安全に受理できる差分だけをapplyする。service restartやrebootが必要でも、Talosの結果とhealthを観測する。disk layoutなど破壊的になり得る差分をTartのcompatibility tableで推測して適用しない。

### Kubernetes

Kubernetes versionはTalos OS versionとは独立したCAPI desired stateである。Control Plane ProviderがCAPIのversion、version skew、TalosのKubernetes upgrade semanticsを整合させ、control planeとworkerが同じ宣言状態へ収束するまで管理する。

### Rolling update

複数Machineの更新順序と停止数はCAPIのrollout policyへ従う。Tartは独自の`maxUnavailable`やrollout controllerを作らない。single nodeではdowntimeを許容するが、同じMachine、TartMachine、TartHost、diskを維持したままupgradeとrebootを行う。

通常updateでCAPI Machine replacementへ進まない。in-placeにできない差分は`UnsafeChange`、`RequiresExplicitReprovision`、`Blocked`として停止する。

## Deletion

```text
Machine削除
    ↓
TartMachine finalizer
    ↓
Host claim解除
    ↓
Host resourceと物理dataを保持
    ↓
明示的な再利用またはreprovisioningを待つ
```

Machine deletionはユーザーが明示したlifecycleだが、`TartMachine` deletionをdisk wipeとは解釈しない。Hostの再利用やcleaningを追加する場合は、通常updateとは別の権限、確認、監査、Conditionを持つ明示的なoperationとして設計する。

## Recoveryとerror

power off、DHCP address待ち、maintenance API待ち、Talos reboot、Kubernetes APIの一時的unavailableはreconcile可能なtransient stateとして扱う。identity mismatch、invalid selector、destructive change、quorum violation、unsupported update pathはbounded retryを続けず、外部副作用を止めたblocked stateへ反映する。

外部API call直後にcontrollerが停止しても、再起動後はversion、reachability、health、configuration digest、ProviderID、etcd membershipを観測して未開始・実行中・完了済み・失敗を判断する。API callを送った事実だけをStatusへ保存しない。
