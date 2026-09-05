# Cluster API Provider Tart

Tartは、Talos Linuxを実行する物理または仮想HostをCluster APIへ統合するProviderです。Infrastructure Provider、Bootstrap Provider、Control Plane Providerを提供し、Host allocation、boot、Talos configuration delivery、control plane lifecycle、安全なin-place updateを担当します。

TartはTalos専用です。TalosのOS installation、disk/volume、machine configuration、upgrade、rollback、etcd bootstrap、Kubernetes runtimeを再実装せず、Talos APIとmachine configurationへ委譲します。Cilium、Longhorn、TopoLVM、kube-vipなどのadd-on専用APIは提供しません。

現在は新アーキテクチャの再実装中です。APIは`v1alpha1`へリセットし、Infrastructure、Bootstrap、Control Planeをそれぞれ`infrastructure.cluster.x-k8s.io`、`bootstrap.cluster.x-k8s.io`、`controlplane.cluster.x-k8s.io`へ分けています。過去の`v1beta1` APIやProvisioning Agentとの互換性はありません。

## ドキュメント

- [利用者向けドキュメント](docs/README.md): 利用前提と提供状況
- [開発者向けドキュメント](docs/development/README.md): 設計、実装、生成、検証
- [アーキテクチャ](docs/development/architecture.md): Provider、package、依存方向、副作用境界
- [API contract](docs/development/api-contract.md): ResourceとCAPI contract
- [Machine lifecycle](docs/development/lifecycle.md): provisioning、update、deletion、recovery
- [Talos連携](docs/development/talos.md): configuration、storage、upgrade、add-on
- [セキュリティと観測性](docs/development/security.md): maintenance trust、Secret、MHC、Runtime Extension

## 開発

開発環境と検証コマンドは[開発ガイド](docs/development/development.md)を参照してください。Go testは全面禁止せず、Host claim race、unsafe diff、quorum判定、外部contractなど失敗時の影響が大きい境界へ限定して実装します。Talos、storage、reboot、rollback、drain、CAPI minorごとのreplacement不発は実機E2Eなど適切な境界で検証し、静的確認とruntime受け入れを分けて扱います。
