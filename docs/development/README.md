# 開発者向けドキュメント

Tartが達成すべき要件とユースケース、および非目標を記す。実装方針・API契約・アルゴリズムの詳細はコードとそのコメント([`api/`](../../api)、[`controller/`](../../controller)、[`talos/`](../../talos)等)を正本とし、ここには複製しない。設計・実装レビューの着眼点は[`.agents/skills/`](../../.agents/skills)を参照する。

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

## 現状の既知の制約

- 実機・VMでの動作確認は未実施(検証はユーザー自身が行う)。対応version matrix(CAPI minor、Talos minor、Kubernetes version range)は未定義。
- node cordon/drain(`extensions/drain.go`)には専用のtest coverageがなく、drain timeoutは大規模clusterのdrain所要時間を考慮していない。

## 静的検証

コード変更時は`mise run fmt`、`mise run generate`、`mise run manifests`、`mise run lint:fix`、`mise run test`を実行し、生成物はcontroller-gen/kustomizeで再生成する(手編集しない)。
