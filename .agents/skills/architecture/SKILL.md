---
name: architecture
description: TartのTalos専用アーキテクチャと責務境界を確認する
when_to_use: Resource、Provider、controller、外部adapterの設計・実装・レビューを行う時
---

# Tartアーキテクチャ

## 先に読む文書

設計・実装・レビューを始める前に、次の順で読む。

1. [`docs/development/README.md`](../../../docs/development/README.md)
2. [`docs/development/architecture.md`](../../../docs/development/architecture.md)
3. [`docs/development/api-contract.md`](../../../docs/development/api-contract.md)
4. [`docs/development/lifecycle.md`](../../../docs/development/lifecycle.md)
5. [`docs/development/talos.md`](../../../docs/development/talos.md)
6. [`docs/development/decisions.md`](../../../docs/development/decisions.md)
7. [`docs/development/verification.md`](../../../docs/development/verification.md)

各文書は責務ごとの設計の正本であり、`README.md`が参照関係の入口である。古いコード、旧API、旧ドキュメントに合わせるために新設計を曲げない。

## 基本方針

- TartはTalos Linux専用であり、Kubeadm、Ubuntu、汎用OS provisioning framework、既存Talos Providerには依存しない。
- Infrastructure Provider、Bootstrap Provider、Control Plane Providerを提供する。
- TalosのOS installation、machine configuration、disk/volume、upgrade、rollback、etcd bootstrap、Kubernetes runtimeへ責務を委譲する。
- CAPI Machineを使い捨てと仮定せず、同じMachine、TartMachine、TartHost、diskを保ったin-place updateを第一選択にする。
- 安全にin-place updateできない変更はMachine replacementへ暗黙にfallbackせず、blocked Conditionで停止する。
- Resource Statusは外部から観測できる状態とConditionだけを持ち、workflowのstep番号やprogram counterを持たない。
- controller再起動後も、Kubernetes desired stateとHost/Talos/Kubernetesのobserved stateから同じ判断を再計算できるようにする。

## ディレクトリ方針

ルート直下の責務別パッケージだけを使う。`internal`と`pkg`は禁止する。`domain`、`infrastructure`、`workflow`のように複数の責務を隠す大分類も作らない。

```text
api/v1alpha1             CRD型
controller               Kubernetes reconcile entrypoint
host                     Host allocation、claim、identity
talos                    Talos API adapter
bootstrap                Talos configuration生成とpatch合成
controlplane             etcd/control plane policy
boot                     maintenance boot backend
extensions               CAPI Runtime Extension
cmd/controller-manager   process wiring
```

これらは必要になった責務の置き場であり、全てを事前に抽象化するための雛形ではない。interfaceは実際に複数実装がある、または副作用を隔離する境界でのみ作る。

## 禁止事項

- `TartHostOperation`、Operation CRD、Workflow engine、Provisioning Planを追加しない。
- 独自Provisioning Agent、Node Lifecycle Agent、OS image format、disk writer、partition DSL、A/B updater、rollback managerを追加しない。
- Cilium、Longhorn、TopoLVM、kube-vipなどのadd-on専用APIを追加しない。
- DHCP、TFTP、PXE、BMC、VM APIの具体的な方式をTartのdomain modelやCRDへ固定しない。
- local persistent dataを持つ可能性があるHostを、通常のtemplate差分だけでcleaning、reprovisioning、disk wipeしない。

## 変更時の確認

CRD、Provider contract、controllerの変更時は、設計の責務表、正本表、削除・更新の安全性、secret境界、controller再起動後の再計算可能性を確認する。詳細は[API contract](../../../docs/development/api-contract.md)、[Machine lifecycle](../../../docs/development/lifecycle.md)、[検証方針](../../../docs/development/verification.md)による。
