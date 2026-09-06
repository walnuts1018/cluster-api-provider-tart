# Cluster API Provider Tart

Tartは、Talos Linuxを実行する物理または仮想HostをCluster APIへ統合するProviderです。Infrastructure Provider、Bootstrap Provider、Control Plane Providerを提供し、Host allocation、boot、Talos configuration delivery、control plane lifecycle、安全なin-place updateを担当します。

TartはTalos専用です。TalosのOS installation、disk/volume、machine configuration、upgrade、rollback、etcd bootstrap、Kubernetes runtimeを再実装せず、Talos APIとmachine configurationへ委譲します。Cilium、Longhorn、TopoLVM、kube-vipなどのadd-on専用APIは提供しません。

Provider APIは`v1alpha1`であり、Infrastructure、Bootstrap、Control Planeをそれぞれ`infrastructure.cluster.x-k8s.io`、`bootstrap.cluster.x-k8s.io`、`controlplane.cluster.x-k8s.io`へ分けています。

## ドキュメント

- [利用者向けドキュメント](docs/README.md): 導入手順と提供状況
- [開発者向けドキュメント](docs/development/README.md): 達成すべき要件・ユースケースと非目標

## 開発

Go、Kubebuilder、controller-gen、kustomize、lint toolのversionは`mise.toml`で管理する。`mise install`後、`mise run fmt`/`generate`/`manifests`/`lint:fix`/`test`で静的検証する。詳細は[`AGENTS.md`](AGENTS.md)を参照。
