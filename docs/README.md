# Tartドキュメント

TartはTalos Linux専用のCluster API Providerです。

## 利用者向けドキュメント

導入手順は[Talos Linux導入](installation/talos.md)を参照してください。ただし実機E2Eによる受け入れ確認は未実施であり、対応version matrix([development/compatibility.md](development/compatibility.md))は未定義です。現時点のreleaseは理論上動作する実装を提供するものであり、実機での動作保証はまだありません。

## 開発者向けドキュメント

開発・設計に関するドキュメントは [開発者向けドキュメント目次](development/README.md) を参照してください。

- [アーキテクチャ](development/architecture.md): Provider全体の構成、依存関係、責務境界
- [API contract](development/api-contract.md): CAPI v1beta2 contractとの統合仕様、不変条件
- [Machine lifecycle](development/lifecycle.md): Provisioning、更新、削除、Host retentionの状態遷移
- [実装タスク一覧](development/tasks.md): **未実装・仮実装機能の要件と解消条件**
- [Talos連携](development/talos.md): Talos Linuxへの委譲方針、Configuration合成
- [セキュリティと観測性](development/security.md): Secret保護規約、RBAC権限境界、観測性
- [設計判断と非目標](development/decisions.md): アーキテクチャ上の設計判断（ADR）と非目標
- [リソースとProvisioningの流れ](development/resources-and-provisioning.md): リソース参照構造と全体フロー
- [検証方針](development/verification.md): 静的検証、単体テスト、将来の受け入れ条件

## 基本アーキテクチャ

Provider APIは`v1alpha1`へリセットし、Infrastructure、Bootstrap、Control Planeを分離してCAPIの現行v1beta2 contractへ適合させています。
CAPI Machineを使い捨てと仮定せず、同一Machine/Host/Disk上でのin-place updateを優先し、安全に実行できない変更はMachine replacementへ暗黙にフォールバックせず安全停止（Fail-Closed）します。
詳細な仕様や未実装タスクについては上記の各ドキュメントを参照してください。
