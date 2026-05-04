---
name: cluster-api
description: Cluster APIのCRDやControllerなどを実装する時のルール
when_to_use: Cluster APIに関連する実装を行う時
---

Cluster APIのCRDやControllerなどを作成する際は、[公式ドキュメント](https://cluster-api.sigs.k8s.io/) をよく読んで、これらの使用に準拠するようにして下さい。

## 主要なドキュメントと要約

### 1. [Provider Overview](https://cluster-api.sigs.k8s.io/developer/providers/overview)

インフラストラクチャプロバイダーの役割と、開発の全体像を説明しています。

- **役割**: VM、ロードバランサー、ネットワークなどのクラウド固有リソースのライフサイクル管理。
- **開発ツール**: `kubebuilder` によるプロジェクト初期化、`Tilt` による高速な開発サイクルが推奨されます。

### 2. [Getting Started](https://cluster-api.sigs.k8s.io/developer/providers/getting-started/overview)

プロバイダー実装の最初の一歩から詳細な実装ガイドです。

- [Naming Conventions](https://cluster-api.sigs.k8s.io/developer/providers/getting-started/naming): リポジトリ名は `cluster-api-provider-${env}`、略称（CAPA, CAPG等）の利用。
- [Implement API Types](https://cluster-api.sigs.k8s.io/developer/providers/getting-started/implement-api-types): `Spec`（望ましい状態）と `Status`（現在の状態）の明確な分離。`runtime.NewSchemeBuilder` による依存関係の最小化。
- [Webhooks](https://cluster-api.sigs.k8s.io/developer/providers/getting-started/webhooks): 不正な設定の拒否（Validation）、デフォルト値の設定（Defaulting）、複数バージョン間の変換（Conversion）。
- [Controllers and Reconciliation](https://cluster-api.sigs.k8s.io/developer/providers/getting-started/controllers-and-reconciliation): べき等性の確保、`OwnerReference`（Cluster等）の適切な処理、`patch.NewHelper` による状態更新。

### 3. [Contracts](https://cluster-api.sigs.k8s.io/developer/providers/contracts/overview)

Core CAPI と連携するためにプロバイダーが遵守すべき「契約」です。

- [Infrastructure Cluster Contract](https://cluster-api.sigs.k8s.io/developer/providers/contracts/infra-cluster):
- [Infrastructure Machine Contract](https://cluster-api.sigs.k8s.io/developer/providers/contracts/infra-machine):
- [Infrastructure MachinePool Contract](https://cluster-api.sigs.k8s.io/developer/providers/contracts/infra-machinepool): マシンのグループ管理（任意/実験的）。

### 4. [Best Practices](https://cluster-api.sigs.k8s.io/developer/providers/best-practices)

- **リソース識別**: 名前ではなく、タグやラベルを用いてクラウドサービス上のリソースを識別する。
- **可観測性**: 状態を Conditions や Events として公開し、トラブルシューティングを容易にする。
- **コントローラーの調整**: 並列実行数やレート制限を適切に設定する。

### 5. [Security Guidelines](https://cluster-api.sigs.k8s.io/developer/providers/security-guidelines)

- **最小権限**: プロバイダーが使用する資格情報には必要最小限の権限のみを付与する。
- **機密情報の保護**: Bootstrap Data（SSHキー等を含む）をセキュアに扱い、使用後はクリーンアップする。
- **レート制限**: クラウドAPIへの過剰なリクエストを防ぎ、クォータ超過やDOSを防ぐ。

## 実装上の注意点

- **べき等性**: `Reconcile` 関数は何度実行されても同じ結果になるように実装してください。
- **状態の永続化**: メモリ内に状態を持たず、必ず Kubernetes オブジェクトの `Status` や `Spec` に保存してください。
- **エラー処理**: 一時的なエラーは `requeue` し、致命的なエラーは `Conditions` でユーザーに通知してください。
