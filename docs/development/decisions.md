# 設計判断と完成条件

この文書は、Tartを一度に新実装へ置き換えるために確定した判断と、実用可能とみなす境界をまとめる。過去実装との互換性を維持するための制約はない。

## 採用する判断

1. Tart独自APIは`v1alpha1`へリセットする。Infrastructure、Bootstrap、Control PlaneのAPI groupは`infrastructure.cluster.x-k8s.io`、`bootstrap.cluster.x-k8s.io`、`controlplane.cluster.x-k8s.io`へ分ける。
2. CAPI coreの現行`v1beta2` contractへ適合し、CAPIの標準lifecycle、Conditions、references、ClusterClass、Runtime Extensionを利用する。
3. Talosのinstaller、configuration、storage、upgrade、rollback、etcd bootstrapを再実装しない。
4. HostはMachineより長寿命のcluster-scoped inventoryとし、Kubernetes metadata UIDから独立したimmutableな`TartHost.spec.id`を持つ。Workload clusterの永続identityにはCAPI `Cluster.metadata.uid`ではなくimmutableな`TartCluster.spec.id`を使う。MAC address、system UUIDなどのstable identityが重複した場合は関係するHostを`Ready=False`、`Reason=IdentityConflict`としてallocationとmaintenance configuration applyを停止する。controller-managedな`TartHost.spec.consumerRef`でclaimをatomic CASにより排他管理する。Machine削除時は`spec.retainedFrom`へ直前のconsumer UIDとcluster IDを残し、freshなHostとretained Hostを再起動後も区別する。`Status`は観測だけを持つ。
4の補足: `TartHost.spec.id`と`TartCluster.spec.id`はTemplateやSSA dry-runのdefaultingで生成せず、concrete Resourceのnon-dry-run CREATE後にprovider controllerが一度だけ生成して永続化する。DR復元では既存値を保持し、ID確定前にbundle生成、Host claim、provisioningを開始しない。同名Clusterの再作成では新しいCluster IDを使う。
5. `Retained` Hostは`Available`へ自動復帰させない。現在の`retainedFrom`に一致する`reuseApproval`、`reusePolicy: Reusable`、`reuseMode: Adopt|Reprovision`が明示された場合だけ`Reusable`にし、`Adopt`とdata破棄を伴う`Reprovision`を別のlifecycleとして扱う。
6. Machine deletionのdrainとvolume detachはCAPI Machine controller、scale-down時のetcd detachはControl Plane Providerのpre-terminate delete hook、Talos shutdownと停止確認・retention・claim処理はTartMachine finalizerが担当する。Cluster全体の削除ではetcd quorum維持を必須にしない。multi-nodeのnode-disruptive updateはdrain成功を必須とし、single-nodeはcordonとgraceful evictionを可能な範囲で試した後、`TartCluster.spec.updatePolicy.allowDowntime: true`が明示されている場合だけdata preservationを優先する。cluster secret bundleはCluster IDを含むgeneration単位でimmutableにし、CA rotationはTalosの準備結果から得た新しいsecret materialで次generationを`Pending`として先に永続化してから段階的なoperationへ委譲し、正常完了後にactive generationを切り替える。Cluster存続中は過去generationをGCせず、bundle消失後のRetained Hostは`Adopt`不可、`Reprovision`専用とする。
7. Tart-managed Machineはlocal stateの有無を判定せず、MHCのdelete-and-recreate remediationを既定で許可しない。初期運用はMachineSetまたはControl PlaneのMachine templateへMachine生成前から`cluster.x-k8s.io/skip-remediation`を設定し、Machine作成後の後追いannotationだけに依存しない。Tart v1alpha1では自動replacementのopt-inを提供せず、再構築はMachineの明示的削除とRetained Hostの`Reprovision`承認で開始する。
8. ProviderIDはHost allocation後に`TartHost.spec.id`から`tart://host/<TartHost.spec.id>`として決定し、Infrastructure ProviderとBootstrap Providerで同じ決定論的な生成規則を共有する。Host allocationとDiscovery bootはbootstrap dataを待たず、Talos provisioningだけがbootstrap dataを待つ。Kubernetes objectを復元してmetadata UIDが変わってもProviderIDを変えない。
9. in-place updateできない変更はMachine replacementへ暗黙にfallbackせず、`Ready=False`と安全なreasonによる明示的な安全停止にする。`CanUpdateMachineSet`/`CanUpdateMachine`はold/new双方の`configSecretRef`をresolveしてeffective configurationをrenderし、Secret参照名ではなくdesired diff全体をcoverできる安全な差分だけを`Success`と完全なpatchで返す。missing、unreadable、generation不明を含むunsafe、unknown、partial diffはpatchなしの`Failure`とする。
10. 初回provisioning後のmutableなTalos OS/config変更を実行できるのはUpdate Extensionだけとし、Infrastructure/Bootstrapの通常reconcileは観測とStatus反映だけを行う。Control Plane Providerは`CanUpdateMachine`成功後にMachine、InfraMachine、BootstrapConfigをannotation付きで更新し、Machineへ`UpdateMachine` hook pendingを設定する。
11. Talos configurationの全機能をraw patchで利用可能にするが、ユーザーのraw patchは全てimmutable Secret-backed inputから読み込み、CRD Specへinline保存しない。Provider-owned invariantとの競合は黙って上書きせず`Ready=False`、`Reason=ConfigurationConflict`にする。
12. cluster-level Talos/Kubernetes secret bundleは`TartCluster.spec.id`を含むgeneration単位でimmutableに生成し、active generationを切り替え可能にする。CA rotationはTalosの準備結果から得た新しいsecret materialで次generationを`Pending`として先に永続化し、accepted CA追加、issuing CA切替、certificate refresh、旧CA削除の段階的operationへ委譲する。正常完了を観測してから新generationをactiveに確定し、Cluster存続中は過去generationをGCしない。Bootstrap Providerはread-onlyで参照し、Bootstrap Secretとworkload kubeconfigはCAPIのSecret contractに従う。
13. `TartMachine.spec.talosImage`の`{version, schematicID}`をTalos imageとsystem extensionの単一の正本にする。boot assetとinstaller imageには同じschematicを使う。
14. Kubernetes desired versionはCAPI `Machine.spec.version`とControl Plane resourceを正本とし、BootstrapConfigへ重複させない。Talosのcluster-wide `upgrade-k8s`とCAPI worker version propagationを、Topology managedとdirectly managedの両方で明示的な収束規則に接続する。
15. Resource Statusはobserved stateとConditionsだけを持ち、Operationやworkflowのprogram counterを保存しない。
16. ルート直下の責務別packageだけを使い、`internal`と`pkg`、巨大な`domain`、`infrastructure`、`workflow`階層を作らない。
17. Go testを全面禁止せず、実装と同時に破壊的な判断、外部contract、controller再起動後の再計算を検証する最小限のtable test、fuzz test、契約テストを追加する。設定ファイルの存在確認やmock呼出し順だけのテストは追加しない。

## Rolloutの標準方針

Workerの対応するCAPI `RollingUpdate`設定では`maxSurge: 0`、`maxUnavailable: 1`を標準profileとする。`OnDelete` strategyは自動worker in-place update lifecycleとしてサポートしない。これはCAPIのrollout controllerを置き換えるものではなく、追加Hostなしに既存Machineを一台ずつin-place updateするための推奨設定である。`maxUnavailable: 0`ではCAPIがbufferのためsurge Machineを作成し得るため、local persistent Hostを守る既定値にしない。Control Planeのin-place updateはTartControlPlaneが一台ずつに固定する。multi-nodeはdrain成功を必須とし、single nodeはcordonとgraceful evictionを可能な範囲で試した後、`TartCluster.spec.updatePolicy.allowDowntime: true`が明示されていればpersistent data preservationを優先してdowntimeを許容する。未指定または`false`なら安全停止する。

## Kubernetes upgradeの収束

Topology managed clusterではCAPI upgrade planのcontrol-plane/worker stepとversion skewが目標versionに整合していれば、現在のworker desired versionが旧versionでもTalos `upgrade-k8s`を開始できる。directly managed clusterでは`TartControlPlane.spec.version`の変更をtriggerにし、worker desired versionが目標versionと矛盾する場合だけ開始前に`Ready=False`、`Reason=VersionSkew`にする。Talos `upgrade-k8s`はcluster-wide operationなのでMachineDeploymentの`maxUnavailable`でavailability sequencingを制御しない。開始後はcluster単位で一度だけ要求し、API server、全Nodeのkubelet、control plane actual versionが目標versionへ収束したことを観測してからControl Planeの`status.versions`を更新する。Topologyではその後CAPIがworkerの`Machine.spec.version`を伝播させ、directly managedでは利用者がworker desired versionを更新する。workerはactual versionが既に目標versionなら重複upgradeなしで完了する。

full machine configurationの再applyで古いKubernetes component imageへdowngradeしないよう、CAPI versionから導出するversion-managed fieldをgeneric user patchから分離する。

## 作らないもの

```text
TartHostOperation
Operation CRD
Workflow engine
Provisioning Plan
独自Provisioning Agent / Node Lifecycle Agent
独自OS image format / disk writer / partition DSL
独自A/B updater / rollback manager
Cilium / Longhorn / TopoLVM / kube-vip専用API
```

DHCP、TFTP、PXE、BMC、VM APIは必要なbackendとして統合できるが、TartのResource semanticsやTalos domain modelの中心へ固定しない。

## Cluster移動とpause

`clusterctl move`でclaimedな`TartHost`を別management clusterへ移動することは、Tart v1alpha1の対応範囲外とする。`TartHost`はCAPI Machineにownerされない長寿命inventoryであり、Hostの物理data、consumer binding、Talos credentialsを自動移動・再接続する契約が別途必要なためである。代わりにmanagement cluster DRでは`TartHost.spec.id`、CAPI Resource、consumer/retention binding、全secret bundle generation、power/boot設定を同じ整合点からバックアップし、復元後にobserved stateを再確認する。対応していない移動は事前に`Ready=False`と安全なreasonで報告し、Host claimを解除して移動を成立させるfallbackを行わない。

`cluster.x-k8s.io/paused`がClusterまたは対象provider resourceに設定された場合、Tart controllerは外部副作用を開始せず、既存のHost claim、Retained記録、Talos installation、dataをそのまま保持する。pause解除後はKubernetes、Host、Talosのobserved stateからreconcileを再開する。pauseをshutdown、release、cleanの指示として解釈しない。

Control plane endpointはTartがVIPをallocateせず、利用者、IPAM、または別Infrastructure ProviderがCAPI `Cluster.spec.controlPlaneEndpoint`へ設定する。TartControlPlaneはendpointが設定されるまでreconcileを進めず、kube-vipなどの実装を専用APIとして所有しない。

対応versionはTart releaseごとにCAPI minor、Talos minor、Kubernetes version rangeのtested matrixを公開する。Runtime SDKとInPlaceUpdatesがexperimentalな間は、matrixにない組み合わせでin-place updateを安全とみなさず、明示的な`UnsupportedVersionCombination`として拒否する。`latest`同士を暗黙に対応範囲へ含めない。

## 将来拡張

Proxmox VM、ARM64、Raspberry Pi、Secure Boot、TPM、attestationを追加できるよう、Host lifecycleの責務境界とTalos image選択の境界を維持する。ただし、現在必要な複数の具体的ユースケースが確認できるまで共通抽象化を作らない。物理HostとVMの差を一つの巨大なinterfaceで隠さない。

## 完成条件

実用可能な最初の実装は、次の受け入れ確認を満たす。

### Enrollment / Discovery

Hostをsecret-freeなmaintenance Talosで起動し、disk UUIDやLinux device pathを事前入力せずhardware inventory、stable identity、disk selectorを`TartHost.status`へ反映する。Discovery bootはBootstrap Secretを待たず、machine configuration applyとinstallはBootstrap Secretを待つ。

### Fresh machine

最小限のHost登録とCAPI Resource作成から、maintenance Talos boot、Hostとのidentity binding、hardware discovery、configuration delivery、Talos installation、authenticated API recovery、Cluster Readyまで進む。install前にdisk UUIDやLinux device pathを要求しない。

### Single node

1 control plane Machineを作成し、Talos OSとKubernetesを同じMachine、Host、diskのまま更新できる。rebootを伴うupdateではmulti-nodeはTalosの安全なdrainまたはworkload cluster側のcordon/drainの成功を必須とする。single-nodeはcordonとgraceful evictionを可能な範囲で試し、`TartCluster.spec.updatePolicy.allowDowntime: true`が明示されていればavailabilityを理由に永久blockせずpersistent data preservationを優先する。未指定または`false`なら開始しない。Machine delete、Host clean、recreateへfallbackせず、Machine削除時には停止確認後に`Retained`となる。

### HAとworker

複数control planeの作成、quorumを守るscale up/down、一台ずつのTalos update、MachineDeploymentからのworker作成、既存Machineを保持したworker updateを確認する。`Machine.spec.failureDomain`を設定した場合は対応するfailure domainのHostへ割り当てられる。

### Storageとadd-on

複数diskについてTalos-native selector、volume、encryptionを利用できる。永続データはUser VolumeまたはRaw Volumeへ配置し、EPHEMERALを保持性の証拠や永続用途として扱わない。Cilium、Longhorn、TopoLVM、kube-vipをTart専用APIなしでTalos configurationとKubernetes manifestから利用できる。

### Recoveryとsafety

provisioning、reboot、upgrade、bootstrap API call直後にcontroller-managerを再起動してもResourceを手動修復せずreconcileが継続する。通常updateでMachine replacement、Host cleaning、disk wipeが起きず、unsafe change、identity mismatch、停止未確認のdeletionが副作用なしで`Ready=False`と安全なreasonになる。multi-nodeのnode-disruptive updateではdrain成功を必須とし、single-nodeではbest-effort drain後の明示的downtime policyを許容する。Talos rollback後はdesiredを自動修正せず後続Control Plane updateを停止する。storage E2Eでは永続volume上のsentinel payloadを更新前後で検証する。

### Contract

Bootstrap Secret、Secret-backed raw configuration、Cluster ID付きgeneration単位のcluster secret bundleとTalos CA rotation、workload kubeconfig、ProviderIDとNodeの一致、`controlPlaneInitialized`とNode Readyの分離、ResourceごとのCondition、ClusterClass SSA dry-run、Runtime ExtensionのTLS接続を受け入れ確認する。

## 現在のテスト方針

Go testは、Host claim race、`Retained` Hostの自動allocation防止、unsafe diffのreplacement防止、reuse approval世代不一致、quorum判定、configuration invariant conflict、semantic digest、Bootstrap Secret contract、bootstrap idempotencyなど、失敗時の影響が大きい境界へ限定する。実機依存のTalos、storage、reboot、rollback、drain、CAPI minorごとのreplacement不発はE2Eで補完する。設定ファイルの存在確認やmock呼出し順だけのテストは追加しない。
