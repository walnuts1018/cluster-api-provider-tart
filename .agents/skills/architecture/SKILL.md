---
name: architecture
description: TartのTalos専用アーキテクチャと責務境界を確認する
when_to_use: Resource、Provider、controller、外部adapterの設計・実装・レビューを行う時
---

# Tartアーキテクチャ規約

Tartのアーキテクチャ設計・実装・レビューを行う際のガイドラインである。
達成すべき要件・非目標は[開発者向けドキュメント](../../../docs/development/README.md)を参照すること。

---

## 基本原則

1. **Talos専用**: Kubeadm、Ubuntu、汎用OSプロビジョニングフレームワークへの互換層は作らない。
2. **三層Provider分離**: Infrastructure、Bootstrap、Control PlaneのAPI groupを分離して提供する。
3. **Talos機能の委譲**: OSインストール、ストレージ、アップグレード、ロールバック、etcd bootstrap、Kubernetes runtimeはTalosへ委譲し、再実装しない。
4. **In-place Update優先**: 同一のMachine、TartMachine、TartHost、diskを維持した更新を第一選択とし、安全に更新できない変更はMachine replacementへ暗黙にフォールバックせず安全停止（Fail-Closed）する。
5. **ディレクトリ規約**: ルート直下の `internal` および `pkg` は禁止する。

---

## 責務境界チェックリスト

設計・実装時に以下の境界が守られているか確認する。

- [ ] Infrastructure ProviderがTalos machine configurationの生成やcluster secretの生成に関与していないか
- [ ] Bootstrap ProviderがOSインストールやHost電源制御を行っていないか
- [ ] Control Plane ProviderがHost inventoryやCNI/CSI add-onを直接管理していないか
- [ ] 純粋なドメイン/ポリシーパッケージ（`host/`, `controlplane/`, `bootstrap/`）がKubernetes clientやTalos内部gRPC型に直接依存していないか
- [ ] 外部副作用（Talos API、電源操作、network boot）がアダプター層（`talos/`, `boot/`, `netboot/`）に隔離されているか
- [ ] `netboot/`・`cmd/netboot-server`がKubernetes APIをread-only（TartHost/TartMachineのget/list/watchのみ）で参照するだけに留まり、Secretアクセスやcontroller-manager同等の権限を持っていないか

---

## 禁止事項

- `TartHostOperation`、Operation CRD、Workflow engine、Provisioning Planを追加しない。
- 独自Provisioning Agent、Node Lifecycle Agent、OS image format、disk writer、partition DSLを追加しない。
- Cilium、Longhorn、TopoLVM、kube-vipなどのadd-on専用APIを追加しない。
- Resource Statusに処理の手順番号や内部ステップを保存しない（観測結果とConditionsのみとする）。

---

## 参照ドキュメント・コード

- 達成すべき要件・非目標: [`docs/development/README.md`](../../../docs/development/README.md)
