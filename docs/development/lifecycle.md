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
TartHost.spec.consumerRefを排他的にclaim
        ↓
Hostを起動し、maintenance Talosのidentityとinventoryを観測
        ↓
Bootstrap Secretのmachine configurationをTalos APIへapply
        ↓
Talos installer、reboot、authenticated API recovery
        ↓
Talos version、health、ProviderID、addressを観測
        ↓
InfrastructureReady / BootstrapReady / Cluster Ready
```

Tartはimageをblock deviceへ直接書き込まず、partition tableを編集せず、OS installerの代わりを実装しない。installation target、volume、encryption、kernel module、mount、extra manifestはTalos-native configurationで指定し、system extensionはTalos image schematicで指定する。

## Host allocation

明示的な`hostRef`を最優先し、未指定ならarchitecture、label、hardware capability、failure domain、availabilityを満たすHostをdeterministicに選択する。`Machine.spec.failureDomain`が設定されている場合、allocatorは一致するHostだけを候補にする。failure domainを正しく観測・割り当てできない場合は、Machineを別domainへ割り当ててはならない。

claimは`TartHost.spec.consumerRef`をcontroller-managed bindingとしてserver-side applyし、namespace、name、UIDとresourceVersionを検証する。`TartHost.status.claimedBy`を排他lockの正本にしない。別MachineがclaimしたHostを上書きせず、競合時は別Hostの選択または再試行を行う。`TartMachine.status.hostRef`はこのbindingの観測である。

Host allocation eligibilityはworkflow phaseではなく、`Available`、`Claimed`、`Retained`、`Reusable`の観測として扱う。`Retained`は前回のMachineに由来するdataやTalos identityが残るため、`Available`ではない。Machine削除後は自動的に`Available`へ戻さず、明示的なreuse/adopt/reprovision/cleanの判断が行われるまでselectorの候補へ入れない。初期実装では安全な既定値を`Retained`とし、`Reusable`への変更をユーザーが明示した場合だけallocationを許可する。

## Fresh provisioningとdeletionの境界

power on、DHCP address待ち、maintenance API待ち、inventory取得、Talos installation、rebootは外部状態を観測しながら再試行する。power onの成功はTalos起動やinstallation完了を意味しない。maintenance endpointとHost identity、MAC/DHCP情報、system UUID、hardware inventoryを結び付け、曖昧な場合はconfigurationをapplyせず停止する。

Machineの削除はupdateとは異なる明示的なlifecycleだが、物理Hostのcleaningやreprovisioningを暗黙に実行しない。削除時の既定フローは次のとおりである。

```text
CAPIのdrain / Node lifecycle完了を待つ
        ↓
control planeならetcd member detachの安全性を確認
        ↓
authenticated Talos APIへshutdown/quiesceを要求
        ↓
Hostが停止したことを観測して確認
        ↓
TartHost.spec.consumerRefを解除
        ↓
TartHostをRetainedとして保持
```

Talos APIへ到達できない、shutdownの結果を確認できない、またはHostが停止していない場合はclaimを解放せず、`Blocked`と安全な理由をConditionへ反映する。明示的なforce releaseを将来追加できるが、通常のMachine削除やupdateから呼び出してはならない。停止確認前にclaimを解放すると、旧clusterのkubelet credentialsを持つHostが稼働したまま別Machineへ割り当てられるためである。

## MachineHealthCheck

Cluster APIのMachineHealthCheckは通常、unhealthy Machineを削除してMachineSetやControl Plane Providerにreplacementを作らせる。これはlocal persistent stateを持つTartMachineの既定remediationとして安全ではない。

local persistent stateを保持するMachineでは、delete-and-recreate remediationを標準経路として利用しない。初期運用では対象Machineへ`cluster.x-k8s.io/skip-remediation`を設定し、MHCによる自動削除を止める。将来のexternal remediationは、同じMachine、TartMachine、TartHostを維持したままpower cycle、Talos reboot、Talos recovery、health確認を行う方式とする。

`skip-remediation`を設定してもHostが自動的に安全になるわけではない。MHC、rollout、手動削除を含む全てのMachine deletion経路で、TartMachine finalizerのshutdown確認と`Retained` gateを通す。通常updateの`CanUpdateMachine=false`だけでCAPIのimmutable replacement fallbackを防げるとはみなさない。

## Control Plane

初回bootstrapは、新規clusterの最初のcontrol plane Machineを選び、Talos etcd bootstrapを未初期化の場合だけ呼び出す。API call直後に完了へ変更せず、Talos etcd membership、workload Kubernetes API、control plane healthを観測する。controller再起動後も観測を先に行い、bootstrap済みclusterへ再初期化を送らない。

`controlPlaneInitialized`は全NodeがReadyであることを意味しない。Talos control planeが起動し、Kubernetes API serverがrequestを受け付けられる時点でtrueにする。CNIが未導入でもこのConditionをtrueにできる。CNIの導入や全NodeのreadinessはClusterResourceSet、Addon Provider、GitOpsなどの後続処理と`Available`/`Ready`で表す。

Control Plane ProviderはCAPI contractに従い、Cluster namespaceへ`<cluster-name>-kubeconfig` Secretを生成・維持する。kubeconfigの生成完了をcontrol plane bootstrapのprocess memoryで管理せず、Secret、API server、証明書の有効期限を観測して再concileする。

scale upは既存control planeがhealthyであることを確認し、新MachineのTalos configuration、reachability、healthを確認してからready countへ反映する。scale downは対象memberを安全にremoveでき、quorumを維持できるとTalos/etcdの観測で判断してからMachine削除へ進む。判定不能または危険な場合は削除せず、`Blocked`または`UnsafeControlPlaneOperation`を設定する。

## Update

Talos OS update、Talos machine configuration update、Kubernetes update、Machine replacementを別のlifecycleとして扱う。通常のTalos/Kubernetes更新では同じCAPI Machine、`TartMachine`、`TartHost`、diskを維持する。

### Talos OSとimage

`TartMachine.spec.talosImage`の`{version, schematicID}`とTalos actual version/schematicを比較し、差分がある場合だけTalos upgrade APIを呼ぶ。system extension setの変更もimage identityの変更として扱い、同じschematicをboot assetとinstaller imageへ使用する。reboot中はtransient stateとして待ち、reboot後にTalos API、version、schematic、healthを再取得する。intermediate version、upgrade failure、rollbackはTalosのsemanticsへ委譲する。

### Machine configuration

configuration digestが変わった場合、Talos APIが安全に受理できる差分だけをapplyする。service restartやrebootが必要でも、Talosの結果とhealthを観測する。disk layoutなど破壊的になり得る差分をTart独自の巨大なcompatibility tableで推測して適用しない。

full machine configurationを再applyする場合は、CAPI `Machine.spec.version`から導出したcurrent Kubernetes versionを必ずeffective configurationへ反映する。Talosの`upgrade-k8s`がlive configurationのKubernetes component image versionを書き換えるため、古いconfiguration digestをそのまま再applyしてdowngradeを発生させてはならない。version-managed fieldはgeneric user patchから分離する。

### Kubernetes

Kubernetes versionはTalos OS versionとは独立したCAPI desired stateであり、`TartBootstrapConfig.spec`へコピーしない。Talosの`upgrade-k8s`がcluster-wideにcontrol planeとworkerのKubernetes componentを更新することを前提に、Control Plane ProviderとCAPI topologyのdesired stateを次のように収束させる。

```text
CAPI upgrade planがversion Xのstepを開始
        ↓
TartControlPlaneがtalos upgrade-k8s Xをclusterごとに一度だけ要求
        ↓
Kubernetes API、全Nodeのkubelet、control plane actual versionがXになることを観測
        ↓
TartControlPlane statusのversionをXへ反映
        ↓
CAPIがworkerのMachine.spec.versionをXへ伝播
        ↓
worker UpdateMachineがactual version Xを観測し、重複upgradeなしで完了
```

upgrade requestの送信事実を正本にせず、API server version、Node version、Talos observed state、Control Plane Statusから未開始・実行中・完了済み・失敗を判定する。version skewを満たさない間は次のupgrade stepへ進めず、workerのdesired stateだけが遅れている一時状態を安全に扱う。

### CAPI in-place updateとrollout

Runtime Extensionの`CanUpdateMachine`は安全なin-place差分だけをtrueとし、`UpdateMachine`では同じMachineへのTalos operationを開始する。CAPIのhook未対応差分はimmutable rolloutへfallbackし得るため、unsupported/destructive changeを通常rolloutへ流さず、対象`TartMachine`を`Blocked`として明示的に停止する。

Tartの標準rollout profileは、対応するCAPI resourceの設定で`maxSurge: 0`、`maxUnavailable: 1`を推奨する。これにより追加Hostを要求せず、一度に一つの既存Machineをin-place updateする。`maxUnavailable: 0`ではCAPIがbufferのためsurge Machineを作成し得るため、local persistent Hostを守る既定値にしない。single nodeでは1台unavailableになるdowntimeを許容し、Machineとdiskを置き換えない。Tart独自のrollout controllerや`maxUnavailable`の別実装は作らない。

## Recoveryとerror

power off、DHCP address待ち、maintenance API待ち、Talos reboot、Kubernetes APIの一時的unavailableはreconcile可能なtransient stateとして扱う。identity mismatch、invalid selector、destructive change、quorum violation、unsupported update path、停止未確認のdeletionはbounded retryを続けず、外部副作用を止めたblocked stateへ反映する。

外部API call直後にcontrollerが停止しても、再起動後はversion、reachability、health、configuration digest、ProviderID、etcd membership、Secret、Node状態を観測して継続する。同じAPI呼び出しを再試行しても安全でない操作は、観測で完了を確認できるadapterだけから呼び出す。
