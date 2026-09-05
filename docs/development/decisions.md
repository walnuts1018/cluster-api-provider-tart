# 設計判断と完成条件

この文書は、Tartを一度に新実装へ置き換えるために確定した判断と、実用可能とみなす境界をまとめる。過去実装との互換性を維持するための制約はない。

## 採用する判断

1. Tart独自APIは`v1alpha1`へリセットする。Infrastructure、Bootstrap、Control PlaneのAPI groupは`infrastructure.cluster.x-k8s.io`、`bootstrap.cluster.x-k8s.io`、`controlplane.cluster.x-k8s.io`へ分ける。
2. CAPI coreの現行`v1beta2` contractへ適合し、CAPIの標準lifecycle、Conditions、references、ClusterClass、Runtime Extensionを利用する。
3. Talosのinstaller、configuration、storage、upgrade、rollback、etcd bootstrapを再実装しない。
4. HostはMachineより長寿命のcluster-scoped inventoryとし、MAC address、system UUIDなどのstable identityをmanagement cluster全体で一意にする。controller-managedな`TartHost.spec.consumerRef`でclaimをatomic CASにより排他管理する。Machine削除時は`spec.retainedFrom`へ直前のconsumer UIDとcluster UIDを残し、freshなHostとretained Hostを再起動後も区別する。`Status`は観測だけを持つ。
5. `Retained` Hostは`Available`へ自動復帰させない。現在の`retainedFrom`に一致する`reuseApproval`、`reusePolicy: Reusable`、`reuseMode: Adopt|Reprovision`が明示された場合だけ`Reusable`にし、`Adopt`とdata破棄を伴う`Reprovision`を別のlifecycleとして扱う。
6. Machine deletionのdrainとvolume detachはCAPI Machine controller、scale-down時のetcd detachはControl Plane Providerのpre-terminate delete hook、Talos shutdownと停止確認・retention・claim処理はTartMachine finalizerが担当する。Cluster全体の削除ではetcd quorum維持を必須にしない。cluster secret bundleは全Managed Machineのshutdownとretention完了後までGCせず、bundle消失後のRetained Hostは`Adopt`不可、`Reprovision`専用とする。
7. Tart-managed Machineはlocal stateの有無を判定せず、MHCのdelete-and-recreate remediationを既定で許可しない。初期運用は`cluster.x-k8s.io/skip-remediation`を利用し、replacementは明示的なopt-inに限定する。
8. ProviderIDはHost allocation後にHost UIDから`tart://host/<TartHost UID>`として決定し、Infrastructure ProviderとBootstrap Providerで同じ決定論的な生成規則を共有する。Host allocationはbootstrap dataを待たずにbindingを確立し、Talos provisioningはbootstrap dataを待つ。
9. in-place updateできない変更はMachine replacementへ暗黙にfallbackせず、明示的なblocked stateへする。`CanUpdateMachineSet`/`CanUpdateMachine`はdesired diff全体をcoverできる安全な差分だけを`Success`と完全なpatchで返し、unsafe、unknown、partial diffはpatchなしの`Failure`とする。
10. 初回provisioning後のmutableなTalos OS/config変更を実行できるのはUpdate Extensionだけとし、Infrastructure/Bootstrapの通常reconcileは観測とStatus反映だけを行う。Control Plane Providerは`CanUpdateMachine`成功後にMachine、InfraMachine、BootstrapConfigをannotation付きで更新し、Machineへ`UpdateMachine` hook pendingを設定する。
11. Talos configurationの全機能をraw patchで利用可能にし、Provider-owned invariantとの競合は黙って上書きせずblockedにする。
12. cluster-level Talos/Kubernetes secret bundleはControl Plane ProviderがClusterごとに一度だけ生成し、Bootstrap Providerはread-onlyで参照する。Bootstrap Secretとworkload kubeconfigはCAPIのSecret contractに従う。
13. `TartMachine.spec.talosImage`の`{version, schematicID}`をTalos imageとsystem extensionの単一の正本にする。boot assetとinstaller imageには同じschematicを使う。
14. Kubernetes desired versionはCAPI `Machine.spec.version`とControl Plane resourceを正本とし、BootstrapConfigへ重複させない。Talosのcluster-wide `upgrade-k8s`とCAPI worker version propagationを、Topology managedとdirectly managedの両方で明示的な収束規則に接続する。
15. Resource Statusはobserved stateとConditionsだけを持ち、Operationやworkflowのprogram counterを保存しない。
16. ルート直下の責務別packageだけを使い、`internal`と`pkg`、巨大な`domain`、`infrastructure`、`workflow`階層を作らない。
17. 新設計を組み立てる期間は新しいGo testを追加せず、Go testも実行しない。静的検証を先に行う。

## Rolloutの標準方針

Workerの対応するCAPI rollout設定では`maxSurge: 0`、`maxUnavailable: 1`を標準profileとする。これはCAPIのrollout controllerを置き換えるものではなく、追加Hostなしに既存Machineを一台ずつin-place updateするための推奨設定である。`maxUnavailable: 0`ではCAPIがbufferのためsurge Machineを作成し得るため、local persistent Hostを守る既定値にしない。Control Planeのin-place updateはTartControlPlaneが一台ずつに固定し、single nodeではdowntimeを許容する。

## Kubernetes upgradeの収束

Topology managed clusterではCAPI upgrade planのcontrol-plane/worker stepとversion skewが目標versionに整合していれば、現在のworker desired versionが旧versionでもTalos `upgrade-k8s`を開始できる。directly managed clusterでは`TartControlPlane.spec.version`の変更をtriggerにし、worker desired versionが目標versionと矛盾する場合だけ開始前にblockedにする。Talos `upgrade-k8s`はcluster-wide operationなのでMachineDeploymentの`maxUnavailable`でavailability sequencingを制御しない。開始後はcluster単位で一度だけ要求し、API server、全Nodeのkubelet、control plane actual versionが目標versionへ収束したことを観測してからControl Planeの`status.versions`を更新する。Topologyではその後CAPIがworkerの`Machine.spec.version`を伝播させ、directly managedでは利用者がworker desired versionを更新する。workerはactual versionが既に目標versionなら重複upgradeなしで完了する。

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

`clusterctl move`でclaimedな`TartHost`を別management clusterへ移動することは、Tart v1alpha1の対応範囲外とする。`TartHost`はCAPI Machineにownerされない長寿命inventoryであり、Hostの物理data、consumer binding、Talos credentialsを自動移動・再接続する安全な契約がまだないためである。対応していない移動は事前にblockedとして報告し、Host claimを解除して移動を成立させるfallbackを行わない。

`cluster.x-k8s.io/paused`がClusterまたは対象provider resourceに設定された場合、Tart controllerは外部副作用を開始せず、既存のHost claim、Retained記録、Talos installation、dataをそのまま保持する。pause解除後はKubernetes、Host、Talosのobserved stateからreconcileを再開する。pauseをshutdown、release、cleanの指示として解釈しない。

Control plane endpointはTartがVIPをallocateせず、利用者、IPAM、または別Infrastructure ProviderがCAPI `Cluster.spec.controlPlaneEndpoint`へ設定する。TartControlPlaneはendpointが設定されるまでreconcileを進めず、kube-vipなどの実装を専用APIとして所有しない。

対応versionはTart releaseごとにCAPI minor、Talos minor、Kubernetes version rangeのtested matrixを公開する。Runtime SDKとInPlaceUpdatesがexperimentalな間は、matrixにない組み合わせでin-place updateを安全とみなさず、明示的な`UnsupportedVersionCombination`として拒否する。`latest`同士を暗黙に対応範囲へ含めない。

## 将来拡張

Proxmox VM、ARM64、Raspberry Pi、Secure Boot、TPM、attestationを追加できるよう、Host lifecycleの責務境界とTalos image選択の境界を維持する。ただし、現在必要な複数の具体的ユースケースが確認できるまで共通抽象化を作らない。物理HostとVMの差を一つの巨大なinterfaceで隠さない。

## 完成条件

実用可能な最初の実装は、次の受け入れ確認を満たす。

### Fresh machine

最小限のHost登録とCAPI Resource作成から、maintenance Talos boot、Hostとのidentity binding、hardware discovery、configuration delivery、Talos installation、authenticated API recovery、Cluster Readyまで進む。install前にdisk UUIDやLinux device pathを要求しない。

### Single node

1 control plane Machineを作成し、Talos OSとKubernetesを同じMachine、Host、diskのまま更新できる。reboot中のdowntimeは許容するが、Machine delete、Host clean、recreateへfallbackしない。Machine削除時には停止確認後に`Retained`となる。

### HAとworker

複数control planeの作成、quorumを守るscale up/down、一台ずつのTalos update、MachineDeploymentからのworker作成、既存Machineを保持したworker updateを確認する。`Machine.spec.failureDomain`を設定した場合は対応するfailure domainのHostへ割り当てられる。

### Storageとadd-on

複数diskについてTalos-native selector、volume、encryptionを利用できる。Cilium、Longhorn、TopoLVM、kube-vipをTart専用APIなしでTalos configurationとKubernetes manifestから利用できる。

### Recoveryとsafety

provisioning、reboot、upgrade、bootstrap API call直後にcontroller-managerを再起動してもResourceを手動修復せずreconcileが継続する。通常updateでMachine replacement、Host cleaning、disk wipeが起きず、unsafe change、identity mismatch、停止未確認のdeletionが副作用なしでblockedになる。

### Contract

Bootstrap Secret、cluster secret bundle、workload kubeconfig、ProviderIDとNodeの一致、`controlPlaneInitialized`とNode Readyの分離、Runtime ExtensionのTLS接続を受け入れ確認する。

## 現在のテスト方針

新実装を一気に組み立てる間は、新しいGo testを追加せず、Go testも実行しない。format、生成、build、vet、lint、manifest検証、契約文書の静的確認を先に行う。

方針を解除した後に追加するテストは、Host claim race、`Retained` Hostが自動allocationされないこと、unsafe diffがreplacementへ進まないこと、Bootstrap Secret contract、bootstrapのidempotencyなど、破壊的経路を防ぐ最小限の境界へ限定する。設定ファイルの存在確認やmock呼出し順だけのテストは追加しない。再開時はこの文書と[検証方針](verification.md)、[gotest skill](../../.agents/skills/gotest/SKILL.md)を先に更新する。
