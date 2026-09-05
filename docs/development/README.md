# 開発者向けドキュメント

このディレクトリは、Talos Linux専用のTart Providerを設計・実装・検証するための技術文書である。導入手順は親ディレクトリの[利用者向けドキュメント](../README.md)を参照する。

## ドキュメント一覧

- [アーキテクチャ](architecture.md): Provider全体の構成、依存関係、および責務境界
- [API contract](api-contract.md): CAPI v1beta2 contractとの統合仕様、不変条件、Secret規約
- [Machine lifecycle](lifecycle.md): Provisioning、更新、削除、Host retentionの状態遷移と安全規則
- [実装タスク一覧](tasks.md): **未実装・仮実装機能（Update Extension、Control Plane Reconcileなど）の要件と解消条件**
- [Talos連携](talos.md): Talos Linuxへの委譲方針、Configuration合成、Trust model
- [セキュリティと観測性](security.md): Secret保護規約、RBAC権限境界、Log/Event/Metrics/Tracing
- [設計判断と非目標](decisions.md): アーキテクチャ上の設計判断（ADR）と作らないもの一覧
- [リソースとProvisioningの流れ](resources-and-provisioning.md): Resource参照構造と全体フローの概要
- [開発ガイド](development.md): ツール、コード生成、静的検証、変更手順
- [検証方針](verification.md): 静的検証、単体テスト、将来のE2E受け入れ条件
- [対応version matrix](compatibility.md): CAPI、Talos、Kubernetesの検証済み組み合わせ
- [Release方針](release.md): リリース管理と前提条件

## 基本原則

設計を変更するときは、関連する責務の文書とプロジェクトskillへ同じ変更で反映する。実装済みの型定義や構造はコードベース（[`api/`](../../api)）を正本とし、ドキュメントへ同一構造の複製を行わない。未実装の機能は[実装タスク一覧](tasks.md)でタスクとして管理する。
