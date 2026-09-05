# 設計判断と完成条件

この文書は、Tartを一度に新実装へ置き換えるために確定した判断と、実用可能とみなす境界をまとめる。過去実装との互換性を維持するための制約はない。

## 採用する判断

1. Tart独自APIは`v1alpha1`へリセットする。Infrastructure、Bootstrap、Control PlaneのAPI groupは`infrastructure.cluster.x-k8s.io`、`bootstrap.cluster.x-k8s.io`、`controlplane.cluster.x-k8s.io`へ分ける。
2. CAPI coreの現行`v1beta2` contractへ適合し、CAPIの標準lifecycle、Conditions、references、ClusterClass、Runtime Extensionを利用する。
3. Talosのinstaller、configuration、storage、upgrade、rollback、etcd bootstrapを再実装しない。
4. HostはMachineより長寿命とし、controller-managedな`TartHost.spec.consumerRef`でclaimを排他管理する。`TartHost.spec.reusePolicy`の既定値は`Retain`とし、`Reusable`への変更を明示的な再利用許可とする。`Status`は観測だけを持つ。
5. Machine削除時はdrain、etcd detach、authenticated Talos shutdown、停止確認の後にclaimを解除し、Hostを`Retained`として保持する。`Retained`は`Reusable`へ明示変更されるまで自動allocation不可とする。
6. local persistent stateを持つMachineでは、MHCのdelete-and-recreate remediationを既定で許可しない。初期運用は`cluster.x-k8s.io/skip-remediation`を利用し、将来は同じMachineを維持するexternal remediationを検討する。
7. in-place updateできない変更はMachine replacementへ暗黙にfallbackせず、明示的なblocked stateへする。CAPIのhook未対応差分がimmutable rolloutへfallbackし得ることを前提に、Runtime Extensionだけを安全性の根拠にしない。
8. Talos configurationの全機能をraw patchで利用可能にし、Provider-owned invariantとの競合は黙って上書きせずblockedにする。
9. cluster-level Talos/Kubernetes secret bundleはClusterごとに一度だけ生成し、全BootstrapConfigで共有する。Bootstrap Secretとworkload kubeconfigはCAPIのSecret contractに従う。
10. `TartMachine.spec.talosImage`の`{version, schematicID}`をTalos imageとsystem extensionの単一の正本にする。boot assetとinstaller imageには同じschematicを使う。
11. Kubernetes desired versionはCAPI `Machine.spec.version`とControl Plane resourceを正本とし、BootstrapConfigへ重複させない。Talosのcluster-wide `upgrade-k8s`とCAPI worker version propagationを明示的な収束規則で接続する。
12. Resource Statusはobserved stateとConditionsだけを持ち、Operationやworkflowのprogram counterを保存しない。
13. ルート直下の責務別packageだけを使い、`internal`と`pkg`、巨大な`domain`、`infrastructure`、`workflow`階層を作らない。
14. 新設計を組み立てる期間は新しいGo testを追加せず、Go testも実行しない。静的検証を先に行う。

## Rolloutの標準方針

対応するCAPI rollout設定では`maxSurge: 0`、`maxUnavailable: 1`を標準profileとする。これはCAPIのrollout controllerを置き換えるものではなく、追加Hostなしに既存Machineを一台ずつin-place updateするための推奨設定である。`maxUnavailable: 0`ではCAPIがbufferのためsurge Machineを作成し得るため、local persistent Hostを守る既定値にしない。single nodeではdowntimeを許容する。

## Kubernetes upgradeの収束

Control Plane ProviderはCAPI upgrade planのversion stepごとにTalos `upgrade-k8s`をcluster単位で一度だけ要求する。API server、全Nodeのkubelet、control plane actual versionが目標versionへ収束したことを観測してからControl Plane Statusを更新し、その後CAPIがworkerの`Machine.spec.version`を伝播させる。workerはactual versionが既に目標versionなら重複upgradeなしで完了する。

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
