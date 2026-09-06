# 開発者向けドキュメント

## 達成すべきユースケース

1. 何も入っていないbare-metal PC/rackサーバーがWake-on-LANまたはRedfishで起動し、netboot経由でTalos Image FactoryのPXE配信からkernel/initramfsをロードし、Talosがインストールされ、再起動後にClusterが完成する。
2. Cluster/OSの更新は、停止なし、または計画停止を伴いながらローカルデータ(disk、volume)を保持したまま行える。
3. `clusterctl init`または`InfrastructureProvider`(cluster-api-operator)でインストールしただけで、netboot-serverを含む全コンポーネントが追加のwiringなしに起動する。
4. Cluster API v1beta2 contractへ、Infrastructure/Bootstrap/Control Plane Providerとして統合できる。

## 非目標

- Kubeadm、Ubuntu、汎用OS provisioning framework、既存Talos Providerとの互換層。
- Talosが既に提供するOSインストーラ、machine configuration、disk/volume、upgrade/rollback、etcd bootstrap、Kubernetes runtimeの再実装。
- `TartHostOperation`、Operation CRD、Workflow engine、Provisioning Plan、独自Provisioning Agent、独自Node Lifecycle Agent、独自OS image format/disk writer。
- Cilium、Longhorn、TopoLVM、kube-vipなどのadd-on専用API。
- `clusterctl move`によるclaimed `TartHost`の他management clusterへの移動。
- Control Plane Endpoint VIPの割り当て(IPAM)やkube-vip等の管理。

## サポートversion policy

- Talos:1.14.x。providerはTalos 1.14のmodern multi-document configurationを前提とする。
- Kubernetes workload cluster:1.36.x、1.37.x。
- Cluster API:1.14.1以降の1.14 minor line。
- deprecatedな`.machine.install`/`.machine.kubelet` compatibility pathは提供しない。Talos 1.13以前やdeprecated legacy configurationを持つ既存Machineのmigration/reconciliationは非サポートであり、必要に応じてユーザーがMachineを再作成する。
- 非サポートversionに対するgeneral-purpose runtime rejectionは行わない。非サポートversionでの動作は保証しない。
- future Talos minorはdependency bumpだけで自動的にsupport対象にならない。modern configuration contractの互換性確認とテスト後に個別判断する。

support policyの正本はこのドキュメントであり、`go.mod`/`mise.toml`のdependency pinとは区別する。

## 現状の既知の制約

- 実機・VMでの動作確認は未実施(検証はユーザー自身が行う)。
- node cordon/drain(`controller/runtimeextension/drain.go`)には専用のtest coverageがなく、drain timeoutは大規模clusterのdrain所要時間を考慮していない。

## 静的検証

コード変更時は`mise run fmt`、`mise run generate`、`mise run manifests`、`mise run lint:fix`、`mise run test`を実行し、生成物はcontroller-gen/kustomizeで再生成する(手編集しない)。
