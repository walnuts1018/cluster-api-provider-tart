# 開発者向けドキュメント

このディレクトリは、Talos Linux専用のTart Providerを設計・実装・検証するための技術文書である。導入手順は親ディレクトリの[利用者向けドキュメント](../README.md)を参照する。

- [アーキテクチャ](architecture.md): Provider、package、依存方向、副作用境界
- [API contract](api-contract.md): group分割したv1alpha1 Resource、CAPI v1beta2 contract、Secret、Status、ownership
- [Machine lifecycle](lifecycle.md): provisioning、control plane、update、deletion、recovery
- [Talos連携](talos.md): configuration、storage、hardware discovery、upgrade、add-on
- [セキュリティと観測性](security.md): maintenance trust、Secret、MHC、Runtime Extension、log、Event、metrics、retry
- [設計判断と完成条件](decisions.md): 非目標、将来拡張、受け入れ条件、現在のテスト方針
- [Release方針](release.md): 現在のrelease停止理由と再開条件
- [リソースとProvisioningの流れ](resources-and-provisioning.md): Resourceの正本、Host claim、更新、削除、secret境界
- [開発ガイド](development.md): ツール、生成、静的確認、変更手順
- [検証方針](verification.md): 現在の静的検証と将来のE2E受け入れ条件

設計を変更するときは、関連する責務の文書とプロジェクトskillへ同じ変更で反映する。旧`v1beta1`、Provisioning Agent、Operation、workflow、Ubuntu/kubeadmに関する文書は新設計の根拠として扱わない。
