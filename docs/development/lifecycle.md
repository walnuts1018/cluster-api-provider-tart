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

claimは`TartHost.spec.consumerRef`をcontroller-managed bindingとしてatomic CASで確立する。`GET`で最新resourceVersionを取得し、consumerRefがnilまたは自分のUIDであることを確認した上でresourceVersion付きUpdateを行う。JSON Patchの`test`も利用できる。SSAのfield ownershipを分散lockとして使わず、`TartHost.status.claimedBy`も排他lockの正本にしない。別MachineがclaimしたHostを上書きせず、競合時は別Hostの選択または再試行を行う。`TartMachine.status.hostRef`はこのbindingの観測である。

Host allocation eligibilityはworkflow phaseではなく、`Available`、`Claimed`、`Retained`、`Reusable`の観測として扱う。freshなHostは`spec.consumerRef`と`spec.retainedFrom`がなく、過去のMachine由来のretention記録がないため`Available`である。Machine削除後はcontroller-managedな`spec.retainedFrom`へ直前のconsumer UID、namespace、nameを残し、claimを解除しても`Retained`にする。`Retained`は`Available`ではない。`spec.reusePolicy: Reusable`だけでは不十分で、現在の`retainedFrom`に一致する`spec.reuseApproval.retainedFromUID`と`spec.reuseMode`が明示され、安全条件を再確認できた場合だけ`Reusable`として候補に含める。HostがClaim中またはfreshな間に設定された再利用指定は、将来のMachine削除を承認するものとして扱わない。

`Reusable`には二つの明示的な動作を持たせる。`Adopt`は既存Talos installation、cluster identity、desired configurationが対象Machineと一致する場合だけdataを保持してclaimする。`Reprovision`はdata破棄を明示的に承認する別lifecycleであり、Talosのreset/installer機構へ委譲してから新しいMachineへclaimする。`Reusable`はwipeの同義語ではなく、通常のselector allocation、update、deleteのfallbackからは到達できない。

Host allocationとTalos provisioningを分離する。InfraMachine controllerはCAPI Machineのbootstrap dataを待たずにHostのconsumerRefをCASでclaimし、Host UID由来の`tart://host/<TartHost UID>`をTartMachineとInfraMachineへ設定できる。ただしpower on、maintenance boot、configuration apply、installはBootstrap Secretが利用可能になるまで開始しない。これによりBootstrap Providerは確定済みのHost-based ProviderIDを参照でき、Provider間の循環依存を作らない。

## Fresh provisioningとdeletionの境界

power on、DHCP address待ち、maintenance API待ち、inventory取得、Talos installation、rebootは外部状態を観測しながら再試行する。power onの成功はTalos起動やinstallation完了を意味しない。maintenance endpointとHost identity、MAC/DHCP情報、system UUID、hardware inventoryを結び付け、曖昧な場合はconfigurationをapplyせず停止する。

Machineの削除はupdateとは異なる明示的なlifecycleだが、物理Hostのcleaningやreprovisioningを暗黙に実行しない。CAPI Machine controllerがdrainとvolume detachを先に行い、Control Plane Providerがscale-down時のetcd member removalをpre-terminate delete hookで完了させる。`TartMachine` finalizerはTalos shutdown、停止確認、retention記録、claim処理だけを担当する。削除時の既定フローは次のとおりである。

```text
CAPI Machine controllerがdrain / Node lifecycle / volume detachを完了する
        ↓
scale-downのcontrol planeならpre-terminate delete hookでetcd member removalを完了する
        ↓
authenticated Talos APIへshutdown/quiesceを要求
        ↓
Hostが停止したことを観測して確認
        ↓
TartHost.spec.retainedFromへ直前のconsumerを記録
        ↓
TartHost.spec.consumerRefを解除
        ↓
TartHostをRetainedとして保持
```

Talos APIへ到達できない、shutdownの結果を確認できない、またはHostが停止していない場合はclaimとfinalizerを解放せず、`Blocked`と安全な理由をConditionへ反映する。停止確認の責務はpower capabilityで異なる。BMC/VM backendではout-of-bandの`PowerOff`を確認し、WoL-onlyまたはmanual backendではauthenticated Talos `Shutdown` RPCの受理後にTalos API endpointが一定時間消失したことを観測する。後者は物理電源OFFの証明ではなく、旧clusterへ接続可能なTalosが動作し続けていないことの確認として扱う。明示的なforce releaseを将来追加できるが、通常のMachine削除やupdateから呼び出してはならない。

Tartが自動Reprovisionを提供するHostは、installed OSが存在する状態からremoteにmaintenance environmentへ戻せるboot strategyを持たなければならない。Fresh provisioningだけでnetwork bootできるHostは、明示的なmaintenance boot capabilityが観測できない限りReprovisionの候補にしない。

Cluster全体の削除はscale-downと分ける。Cluster deletionが観測された場合、Control Plane Providerは各etcd memberのquorum維持を最後まで要求せず、pre-terminate hookを安全に完了させてCAPI Machine controllerの削除を進める。個別のscale-downではquorumとmember removalを必須とし、全体削除では削除不能を避けるためmember detachを必須条件にしない。

Cluster secret bundleのOwnerReferenceによるGCは、全Managed Machineのshutdownとretentionが完了するまで許可しない。Cluster削除後にbundleが消えたRetained Hostは旧cluster credentialを復元できないため`Adopt`を禁止し、`Reprovision`だけを許可する。新しいclusterのbundleと新しい通常の`controlplane`/`worker` configurationを用いて再provisionし、既存Talosへauthenticated API接続できない場合は自動wipeせず`Blocked`にする。

## MachineHealthCheck

Cluster APIのMachineHealthCheckは通常、unhealthy Machineを削除してMachineSetやControl Plane Providerにreplacementを作らせる。Tartはadd-onやlocal volumeの種類を知らないため、すべてのTart-managed Machineをnon-replaceableとして扱うことを安全な既定値にする。

初期運用ではすべてのTart-managed Machineへ`cluster.x-k8s.io/skip-remediation`を設定し、MHCのdelete-and-recreate remediationを既定で許可しない。delete-and-recreateを使用する場合は、利用者がMachine単位で明示的にopt-inし、Host上のdataが失われ得ることを承認する。将来の標準remediationは、同じMachine、TartMachine、TartHostを維持したままpower cycle、Talos reboot、Talos recovery、health確認を行うexternal remediationとする。

`skip-remediation`を設定してもHostが自動的に安全になるわけではない。MHC、rollout、手動削除を含む全てのMachine deletion経路で、CAPI Machine controllerの標準drain/volume detach、必要なControl Plane pre-terminate hook、TartMachine finalizerのshutdown確認と`Retained` gateを通す。通常updateの`CanUpdateMachine=false`だけでCAPIのimmutable replacement fallbackを防げるとはみなさない。

## Control Plane

初回bootstrapに先立ち、Control Plane ProviderがClusterごとのcluster secret bundleを一度だけensureする。Bootstrap ProviderはこのSecretをread-onlyで参照する。すべてのcontrol plane Machineへ通常のTalos `controlplane` configurationを適用し、新規clusterの最初のcontrol plane Machineを選んでTalos `Bootstrap` RPCを未初期化の場合だけ呼び出す。API call直後に完了へ変更せず、Talos etcd membership、workload Kubernetes API、control plane healthを観測する。controller再起動後も観測を先に行い、bootstrap済みclusterへ再初期化を送らない。

`controlPlaneInitialized`は全NodeがReadyであることを意味しない。Talos control planeが起動し、Kubernetes API serverがrequestを受け付けられる時点でtrueにする。CNIが未導入でもこのConditionをtrueにできる。CNIの導入や全NodeのreadinessはClusterResourceSet、Addon Provider、GitOpsなどの後続処理と`Available`/`Ready`で表す。

Control Plane ProviderはCAPI contractに従い、Cluster namespaceへ`<cluster-name>-kubeconfig` Secretを生成・維持する。kubeconfigの生成完了をcontrol plane bootstrapのprocess memoryで管理せず、Secret、API server、証明書の有効期限を観測して再concileする。

scale upは既存control planeがhealthyであることを確認し、新MachineのTalos configuration、reachability、healthを確認してからready countへ反映する。control planeのin-place updateは常に一台ずつとし、次のMachineへ進む前にetcd membership、Kubernetes API、Node healthを確認する。3 node以上ではquorumとavailabilityを維持し、single nodeではrebootによるdowntimeを許容する。scale downは対象memberを安全にremoveでき、quorumを維持できるとTalos/etcdの観測で判断してからpre-terminate hookを完了させる。判定不能または危険な場合は削除せず、`Blocked`または`UnsafeControlPlaneOperation`を設定する。

Control Plane Providerが既存Machineを更新する場合、Machine controllerに任せる前に`CanUpdateMachine`を呼ぶ。成功時はCAPI Machine、TartMachine、TartBootstrapConfigのdesired specをresourceVersion付きで更新し、`in-place-updates.internal.cluster.x-k8s.io/update-in-progress` annotationを設定してからMachineへ`UpdateMachine` hook pendingを設定する。再concileではannotation、spec、hook pending、generationを観測してこの遷移を再入可能にし、途中停止後も部分適用を二重実行しない。失敗時やunsupported diffではspecを変更せずControl Planeを`Blocked`にする。

## Update

Talos OS update、Talos machine configuration update、Kubernetes update、Machine replacementを別のlifecycleとして扱う。通常のTalos/Kubernetes更新では同じCAPI Machine、`TartMachine`、`TartHost`、diskを維持する。

### Talos OSとimage

`TartMachine.spec.talosImage`の`{version, schematicID}`とTalos actual version/schematicを比較し、差分がある場合だけTalos upgrade APIを呼ぶ。system extension setの変更もimage identityの変更として扱い、同じschematicをboot assetとinstaller imageへ使用する。reboot中はtransient stateとして待ち、reboot後にTalos API、version、schematic、healthを再取得する。intermediate version、upgrade failure、rollbackはTalosのsemanticsへ委譲する。

### Machine configuration

configuration digestが変わった場合、初回provisioning中だけBootstrap Providerのeffective configurationをinstallへ渡す。初回provisioning後のmutableなconfiguration applyはUpdate Extensionだけが実行し、通常のInfrastructure/Bootstrap reconcileはactual configuration digestとConditionsを観測する。service restartやrebootが必要でも、Talosの結果とhealthをUpdate Extensionから観測する。disk layoutなど破壊的になり得る差分をTart独自の巨大なcompatibility tableで推測して適用しない。

full machine configurationを再applyする場合は、CAPI `Machine.spec.version`から導出したcurrent Kubernetes versionを必ずeffective configurationへ反映する。Talosの`upgrade-k8s`がlive configurationのKubernetes component image versionを書き換えるため、古いconfiguration digestをそのまま再applyしてdowngradeを発生させてはならない。version-managed fieldはgeneric user patchから分離する。

### Kubernetes

Kubernetes versionはTalos OS versionとは独立したCAPI desired stateであり、`TartBootstrapConfig.spec`へコピーしない。Talosの`upgrade-k8s`がcluster-wideにcontrol planeとworkerのKubernetes componentを更新することを前提に、Control Plane ProviderとCAPI topologyのdesired stateを次のように収束させる。

```text
Topology managed clusterでは、CAPI upgrade planがversion Xのcontrol-plane/worker stepと整合する状態を開始
        ↓
TartControlPlaneがupgrade planのstepを確認し、talos upgrade-k8s Xをclusterごとに一度だけ要求
        ↓
Kubernetes API、全Nodeのkubelet、control plane actual versionがXになることを観測
        ↓
TartControlPlane statusのversionをXへ反映
        ↓
CAPIがworkerのMachine.spec.versionをXへ伝播
        ↓
worker UpdateMachineがactual version Xを観測し、重複upgradeなしで完了
```

Topology managed clusterでは、現在のworker `Machine.spec.version`が旧versionであることをupgrade-k8sのprecondition違反とみなさない。CAPIのupgrade planが表すcontrol-plane/worker step、version skew、Topologyの進行方向がtarget Xと整合していればupgrade-k8sを開始できる。Talosのcluster-wide operationであるため、Kubernetes upgrade自体のavailability sequencingをMachineDeploymentの`maxUnavailable`で制御せず、TalosのsemanticsとCAPI upgrade planの境界で扱う。

非Topology managed clusterでは、`TartControlPlane.spec.version`の変更をtriggerとして扱う。Control Plane Providerはworkerの`Machine.spec.version`またはMachineDeploymentのdesired versionが目標versionと矛盾している場合、cluster-wideな`upgrade-k8s`を開始せず`Blocked`にする。Control Plane Providerはworker MachineDeploymentを所有・変更せず、利用者が互換するdesired versionへ更新した後にreconcileする。`upgrade-k8s`後にworkerのactual versionが先に目標へ到達しても、CAPI worker updateはそのactual stateを観測して重複upgradeなしで完了する。

upgrade requestの送信事実を正本にせず、API server version、Node version、Talos observed state、Control Plane Statusから未開始・実行中・完了済み・失敗を判定する。version skewを満たさない間は次のupgrade stepへ進めず、Topology managed clusterではworker desired versionの伝播を待ち、directly managed clusterでは利用者のworker desired version更新を待つ。

### CAPI in-place updateとrollout

Runtime Extensionの`CanUpdateMachineSet`、`CanUpdateMachine`は安全に全desired diffをcoverできる場合だけ`Success`と完全なpatchを返し、危険、未知、または部分的にしかcoverできない差分は`Failure`としてreconcileを停止する。`UpdateMachine`ではUpdate Extensionだけが同じMachineへのTalos operationを開始する。通常のInfrastructure/Bootstrap reconcileはmutable diffを見てもoperationを開始せず、初回provisioning後のconfigurationとimage変更はUpdate Extensionへ任せる。CAPIのhook未対応差分をSuccessの部分patchで通してimmutable rolloutへfallbackさせてはならない。

Workerの標準rollout profileは、対応するCAPI resourceの設定で`maxSurge: 0`、`maxUnavailable: 1`を推奨する。これにより追加Hostを要求せず、一度に一つの既存Machineをin-place updateする。`maxUnavailable: 0`ではCAPIがbufferのためsurge Machineを作成し得るため、local persistent Hostを守る既定値にしない。Control PlaneはprofileをMachineDeploymentへ委譲せず、常に一台ずつ更新してetcd、API、Node healthを確認する。single nodeでは1台unavailableになるdowntimeを許容し、Machineとdiskを置き換えない。Tart独自のrollout controllerや`maxUnavailable`の別実装は作らない。

## Recoveryとerror

power off、DHCP address待ち、maintenance API待ち、Talos reboot、Kubernetes APIの一時的unavailableはreconcile可能なtransient stateとして扱う。identity mismatch、invalid selector、destructive change、quorum violation、unsupported update path、停止未確認のdeletionはbounded retryを続けず、外部副作用を止めたblocked stateへ反映する。

外部API call直後にcontrollerが停止しても、再起動後はversion、reachability、health、configuration digest、ProviderID、etcd membership、Secret、Node状態を観測して継続する。同じAPI呼び出しを再試行しても安全でない操作は、観測で完了を確認できるadapterだけから呼び出す。
