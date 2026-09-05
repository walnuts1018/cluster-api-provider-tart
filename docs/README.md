# Tartドキュメント

TartはTalos Linux専用のCluster API Providerです。現在は新アーキテクチャの再実装中であり、利用者向けの導入手順は実用可能なE2Eが成立した時点で追加します。

## 開発者向け

- [開発者向けドキュメント](development/README.md)
- [アーキテクチャ](development/architecture.md)
- [API contract](development/api-contract.md)
- [Machine lifecycle](development/lifecycle.md)
- [Talos連携](development/talos.md)
- [セキュリティと観測性](development/security.md)
- [設計判断と完成条件](development/decisions.md)
- [Release方針](development/release.md)
- [リソースとProvisioningの流れ](development/resources-and-provisioning.md)
- [検証方針](development/verification.md)

## APIとライフサイクル

Provider APIは`v1alpha1`へリセットし、Infrastructure、Bootstrap、Control Planeをそれぞれ`infrastructure.cluster.x-k8s.io`、`bootstrap.cluster.x-k8s.io`、`controlplane.cluster.x-k8s.io`へ分けます。CAPIの現行v1beta2 contractへ適合させます。通常のTalos/Kubernetes updateは、同じCAPI Machine、`TartMachine`、`TartHost`、diskを維持するin-place updateを第一選択とし、安全に実行できない変更は明示的にblockedとします。Machine削除後のHostは`Retained`として保持し、明示的に`Reusable`へ変更するまで自動allocationしません。
