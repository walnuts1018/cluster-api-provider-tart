# 設計判断と非目標

この文書は、Tartにおいて確定した設計判断の根拠（ADR的背景）、非目標（作らないもの）、および完成条件をまとめる。過去実装（旧v1beta1）との互換性を維持するための制約は存在しない。

---

## 主な設計判断

1. **Talos Linux専用設計**:
   - Kubeadm、Ubuntu、汎用OSプロビジョニングフレームワークへの互換層は作らない。
   - Talos公式のOSインストーラ、machine configuration、storage、upgrade、rollback、etcd bootstrapへ全面的に委譲する。
2. **Provider API groupの分割**:
   - CAPIの責務分割に合わせ、`infrastructure.cluster.x-k8s.io`、`bootstrap.cluster.x-k8s.io`、`controlplane.cluster.x-k8s.io` へ分離して `v1alpha1` へリセットする。
3. **長寿命Host Inventoryと永続Identity**:
   - `TartHost` はmanagement cluster全体で一意なcluster-scoped inventoryとし、Kubernetes metadata UIDから独立したimmutableな `spec.id` を持つ。
   - workload clusterの永続identityとして、CAPI metadata UIDとは独立した `TartCluster.spec.id` を持つ。
   - ProviderIDは `TartHost.spec.id` から決定論的に生成する（`tart://host/<TartHost.spec.id>`）。
4. **In-place Updateの優先とFail-Closed安全停止**:
   - CAPI Machineを使い捨てと仮定せず、同一Machine/Host/Disk上での更新を第一選択とする。
   - 安全にin-place updateできない変更をMachine replacementへ暗黙にフォールバックせず、`Ready=False` で安全停止する。
5. **Host Retentionとデータ保護**:
   - Machine削除後もHostは `Retained` として保持され、明示的な再利用承認（`Adopt` または `Reprovision`）なしに自動割り当てされない。
   - MHCのdelete-and-recreate remediationを既定で抑止するため、Machine生成前から `cluster.x-k8s.io/skip-remediation: "true"` を設定する。
6. **Immutable Secret Input**:
   - ユーザーのraw patchは全て `configSecretRef` のimmutableなSecretから読み込み、CRD Specへのinline保存を行わない。
7. **Stateless Reconcile**:
   - Resource Statusは外部から観測された状態とConditionsのみを保持し、workflowのステップ番号やプログラムカウンタとして利用しない。
8. **Bare-metal初回Bootの所有**:
   - OS未導入のbare-metal HostをTalos maintenance modeまで起動することはInfrastructure Providerの責務とする。
   - 初回bootにはPXE、iPXE、UEFI HTTP Boot等のnetwork bootを利用できるようにするが、具体的なboot方式やDHCP/TFTP等の実装をProvider APIへ固定しない。
   - 初回bootでは原則としてTalosのboot assetのみを配信し、Host確認後にTalos API経由でmachine configurationを適用してインストールを開始する。

9. **Talos upstream実装の直接利用とadapter隔離**:
   - 「Talosが既に実装しているlifecycle logicを再実装しない」という原則は、Talosの安定した公開RPCだけを使うという意味ではない。必要な機能がTalosの公開RPCだけで表現できない場合、Talos upstreamの既存Go実装を直接利用してよい。
   - 具体例として、cluster-wide Kubernetes upgradeに対応する単一のgRPC RPCはTalos machine APIに存在せず、正本の実装は`talosctl upgrade-k8s`が呼ぶ`github.com/siderolabs/talos/pkg/cluster/kubernetes`である。Tartはこのalgorithmをコピーも再実装もせず、upstream実装をそのまま呼び出す。
   - upstreamへの依存は薄いadapter(`talos.KubernetesUpgradeRunner`と`talos/kubernetes_upgrade.go`)へ隔離し、`go.mod`で`github.com/siderolabs/talos`を`github.com/siderolabs/talos/pkg/machinery`と同じversionへpinして同期させる。
   - TalosはTart専用の外部contractではないため、upstream APIの破壊的変更は許容し、Talos versionの更新時にadapterだけを追従させる。「安全な公開契約がないから機能を実装しない」という判断は採らない。

各仕様の詳細は、[アーキテクチャ](architecture.md)、[API contract](api-contract.md)、[Machine lifecycle](lifecycle.md)を参照すること。

---

## 運用境界と特殊ケースの判断

### 1. `clusterctl move` の非サポート

- `clusterctl move` によるclaimedな `TartHost` の他management clusterへの移動は、Tart v1alpha1の対応範囲外とする。
- `TartHost` はCAPI Machineに所有されない長寿命インベントリであり、物理データ、consumer binding、Talos認証情報の自動移動・再接続契約が別途必要なためである。
- management clusterの復元には、`TartHost.spec.id`、CAPI Resource、consumer/retention binding、全secret bundle generation、電源設定を同一整合点からバックアップ・復元するDR手順を用いる。

### 2. `cluster.x-k8s.io/paused` への対応

- ClusterまたはProvider resourceに `cluster.x-k8s.io/paused` が付与された場合、コントローラーは外部副作用を即座に停止し、既存のHost claim、Retained記録、Talosインストール、データをそのまま維持する。
- pause解除後は、外部観測状態から安全にreconcileを再開する。pauseをshutdownやrelease、cleanの指示として解釈しない。

### 3. Control Plane Endpoint VIPの非所有

- Tart自身はVIPの割り当て（IPAM）やkube-vip等の管理を行わない。
- 利用者、外部IPAM、または他Providerが設定する `Cluster.spec.controlPlaneEndpoint` をそのまま利用し、未設定の場合は設定されるまでreconcileを進めない。

### 4. 将来拡張における早期抽象化の回避

- Proxmox VM、ARM64、Raspberry Pi、Secure Boot、TPM、アテステーション等の拡張性を考慮した責務境界を維持するが、具体的ユースケースが確立するまで共通の巨大な抽象化インターフェースを作らない。物理HostとVMの差を不必要な単一インターフェースで隠蔽しない。

---

## 作らないもの（非目標）

以下のコンポーネントや仕組みは、Tartの責務外であり作成しない。

```text
TartHostOperation
Operation CRD
Workflow engine / Step executor
Provisioning Plan
独自Provisioning Agent / Node Lifecycle Agent
独自OS image format / disk writer / partition DSL
独自A/B updater / rollback manager
Cilium / Longhorn / TopoLVM / kube-vip等のadd-on専用API
```

- DHCP、TFTP、PXE等を汎用的なnetwork boot基盤として独自開発することは目的としない。ただし、bare-metal HostをTalos maintenance modeまで自動起動するためのnetwork boot機能または既存基盤との統合はTartの責務とする。ProxyDHCP/TFTP/iPXEスクリプト配信は`netboot/`パッケージと独立バイナリ`cmd/netboot-server`として実装済みであり、controller-managerとは別processのアダプターとして動作させ、TartHost/TartMachineのResource modelには組み込まない。
- BMC、VM API等は必要に応じてアダプターとして統合できるが、TartのResource modelの中心へ固定しない。

---

## 完成条件と未実装タスク

実用可能なプロバイダーとしての完成条件、および現在残っている未実装・仮実装機能の詳細な要件は、[実装タスク一覧](tasks.md)および[検証方針](verification.md)を参照すること。
