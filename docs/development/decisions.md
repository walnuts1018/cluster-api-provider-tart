# 設計判断と完成条件

この文書は、Tartを一度に新実装へ置き換えるために確定した判断と、実用可能とみなす境界をまとめる。過去実装との互換性を維持するための制約はない。

## 採用する判断

1. Provider APIは`infrastructure.cluster.x-k8s.io/v1alpha1`へリセットする。
2. CAPI coreの現行v1beta2 contractへ適合し、CAPIの標準lifecycle、Conditions、references、ClusterClass、Runtime Extensionを利用する。
3. Talosのinstaller、configuration、storage、upgrade、rollback、etcd bootstrapを再実装しない。
4. HostはMachineより長寿命とし、通常updateやMachine削除で物理dataを破棄しない。
5. in-place updateできない変更はMachine replacementへ暗黙にfallbackせず、明示的なblocked stateへする。
6. Talos configurationの全機能をraw patchで利用可能にし、add-on専用APIを作らない。
7. Resource Statusはobserved stateとConditionsだけを持ち、Operationやworkflowのprogram counterを保存しない。
8. ルート直下の責務別packageだけを使い、`internal`と`pkg`、巨大な`domain`、`infrastructure`、`workflow`階層を作らない。

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

最小限のHost登録とCAPI Resource作成から、maintenance Talos boot、hardware discovery、configuration delivery、Talos installation、authenticated API recovery、Cluster Readyまで進む。install前にdisk UUIDやLinux device pathを要求しない。

### Single node

1 control plane Machineを作成し、Talos OSとKubernetesを同じMachine、Host、diskのまま更新できる。reboot中のdowntimeは許容するが、Machine delete、Host clean、recreateへfallbackしない。

### HAとworker

複数control planeの作成、quorumを守るscale up/down、一台ずつのTalos update、MachineDeploymentからのworker作成、既存Machineを保持したworker updateを確認する。

### Storageとadd-on

複数diskについてTalos-native selector、volume、encryptionを利用できる。Cilium、Longhorn、TopoLVM、kube-vipをTart専用APIなしでTalos configurationとKubernetes manifestから利用できる。

### Recoveryとsafety

provisioning、reboot、upgrade、bootstrap API call直後にcontroller-managerを再起動してもResourceを手動修復せずreconcileが継続する。通常updateでMachine replacement、Host cleaning、disk wipeが起きず、unsafe changeは副作用なしでblockedになる。

## 現在のテスト方針

新実装を一気に組み立てる間は、新しいGo testを追加せず、Go testも実行しない。format、生成、build、vet、lint、manifest検証を先に行う。テストを再開する場合は、この文書と[検証方針](verification.md)、[gotest skill](../../.agents/skills/gotest/SKILL.md)を先に更新する。
