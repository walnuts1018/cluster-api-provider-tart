# 実装タスク一覧（未実装・仮実装機能）

この文書は、Tartにおいて現在未実装または仮実装（スタブや`NotImplemented`による安全停止）となっている機能の実装タスク、要件、設計上の注意点、解消条件をまとめた正本である。

すでに実装済みの型定義や基本ロジック（`TartHost`のatomic CAS claim、`TartCluster.spec.id`生成、初期bundle生成、完全構成SecretからのBootstrap Secret生成など）はコード（[`api/`](../../api)、[`host/`](../../host)、[`controller/`](../../controller)等）を参照すること。

---

## 実装状況サマリ

| 機能エリア | 現状 | 該当コード | 主な未実装内容 |
| --- | --- | --- | --- |
| **Runtime Extension (Update)** | 未実装（安全スタブ） | [`extensions/handlers.go`](../../extensions/handlers.go) | safe-diff判定エンジン、パッチ生成、Talos APIによる更新実行 |
| **Control Plane Reconcile** | 一部仮実装 (`NotImplemented`) | [`controller/tartcontrolplane_controller.go`](../../controller/tartcontrolplane_controller.go) | Talos Bootstrap RPC呼び出し、kubeconfig生成、etcd監視、CA rotation |
| **Cluster Reconcile** | 一部仮実装 (`NotImplemented`) | [`controller/tartcluster_controller.go`](../../controller/tartcluster_controller.go) | Ready条件判定、Failure Domain観測・反映 |
| **Machine / Talos Reconcile** | 一部仮実装 (TODOあり) | [`controller/tartmachine_controller.go`](../../controller/tartmachine_controller.go), [`talos/client.go`](../../talos/client.go) | Talos maintenance boot時のdiscoveryとinstall実行、reboot待機、認証済みAPI復帰確認、drain/shutdown完了確認 |
| **Raw Patch 合成** | 暫定実装 (TODOあり) | [`bootstrap/secret.go`](../../bootstrap/secret.go) | cluster bundle、machine context、user raw patch、provider invariantのマージ合成 |
| **Hardware Discovery** | 未実装 | [`controller/tarthost_controller.go`](../../controller/tarthost_controller.go) | Bare-metal Hostのmaintenance Talosからの動的インベントリ取得 |
| **Power Backend** | WoLのみ実装 | [`boot/`](../../boot) | Redfish等のBMCバックエンド |

---

## タスク詳細

### タスク1: Runtime Extension (Update Extension) の実装
- **重要度**: 高
- **現状**: [`extensions/handlers.go`](../../extensions/handlers.go) 内の `canUpdateMachine`, `canUpdateMachineSet`, `updateMachine` が全て `ResponseStatusFailure`（`notImplementedMessage`）を返す安全スタブとなっている。
- **実装内容**:
  1. **safe-diff判定エンジン**:
     - `CanUpdateMachineSet` / `CanUpdateMachine` において、old/new双方の `configSecretRef` を解決し、effective Talos machine configurationをレンダリングする。
     - 差分全体を比較し、安全にin-place updateできる変更（Talos OS image version、破壊的でないmachine configuration変更など）のみを検知する。
     - identity変更、破壊的disk変更、安全性が不明な差分はpatchなしの `Failure` で確実にvetoする（Fail-closed）。
  2. **完全パッチ生成**:
     - 安全と判定された場合、CAPI MachineSet / Machineに対する完全なJSON patchを返却する。
  3. **`UpdateMachine` の実行**:
     - Talos APIを呼び出し、Talos OS upgradeまたはmachine configuration applyを実行する。
     - update実行前にdrainまたはcordonを実施し、Availability理由の失敗時は `TartCluster.spec.updatePolicy.allowDowntime: true` を確認する。
- **解消条件**:
  - in-place update可能な変更に対して `Success` とpatchが返り、`UpdateMachine` でTalos API呼び出しと完了確認が行われること。
  - 不安全な変更に対してCAPIがreplacementにfallbackせず安全停止すること。

### タスク2: TartControlPlane の高度なReconcile実装
- **重要度**: 高
- **現状**: [`controller/tartcontrolplane_controller.go`](../../controller/tartcontrolplane_controller.go) はdesired replicasに基づく子リソース作成を行うが、Talos etcd membership/quorum監視、初回etcd Bootstrap RPC呼び出し、workload kubeconfig生成、quorum-safe scale down、CA rotationが未実装であり、Conditionsに `NotImplemented` が設定されている。
- **実装内容**:
  1. **初回etcd Bootstrap RPC**:
     - 最初のcontrol-plane Machineが起動しmaintenanceから認証済みAPIへ移行した際、Talos `Bootstrap` RPCを一度だけ実行する。
     - API serverが受付可能になった時点で `controlPlaneInitialized` を `True` に設定する。
  2. **Workload kubeconfig Secretの生成と管理**:
     - `<cluster-name>-kubeconfig` Secretを生成し、client certificateの期限監視と更新を行う。
  3. **etcd Quorum監視と安全なScale Down**:
     - scale-down対象Machineの削除時に、pre-terminate delete hook（`pre-terminate.delete.hook.machine.cluster.x-k8s.io/...`）でetcd member removalとquorum維持を確認してから削除を許可する。
  4. **CA Rotationステートマシン**:
     - generation Nからgeneration N+1の `Pending` bundle Secretを先行生成。
     - Talos公式の段階的CA更新（accepted CA追加 → issuing CA切替 → certificate refresh → 旧CA削除）をreconcileし、完了確認後に `activeSecretGeneration` を更新する。
- **解消条件**:
  - `TartControlPlaneAvailableCondition`, `TartControlPlaneEtcdClusterAvailableCondition` 等が実際の観測に基づいて正常に更新されること。

### タスク3: TartCluster のReconcile実装
- **重要度**: 中
- **現状**: [`controller/tartcluster_controller.go`](../../controller/tartcluster_controller.go) では `spec.clusterID` 生成と初期bundle Secret生成のみが行われている。
- **実装内容**:
  1. **Cluster Ready判定**:
     - Control Planeおよび各Infrastructureコンポーネントの観測結果を集約し、Clusterの準備完了を判定してReady Conditionを更新する。
  2. **Failure Domainの観測と伝播**:
     - `TartHost` のFailure Domain設定を集約し、`TartCluster.status.failureDomains` へ反映する。
- **解消条件**:
  - Cluster全体の健全性およびInfrastructure readinessがConditionへ正しく反映されること。

### タスク4: TartMachine のTalosライフサイクル完了処理
- **重要度**: 高
- **現状**: [`controller/tartmachine_controller.go`](../../controller/tartmachine_controller.go) はclaimやProviderID同期を行うが、Talos maintenance boot時のdiscoveryとinstall実行、reboot待機、認証済みAPI復帰確認、drain/shutdown完了確認が未実装。[`talos/client.go`](../../talos/client.go) にもTODO（深い安全ロジック）が存在する。
- **実装内容**:
  1. **Talos Install & Reboot管理**:
     - maintenance modeで起動したHostに対し、Bootstrap Secretから取得したconfigurationをapplyし、installを実行する。
     - reboot後の認証済みAPI到達とversion/schematicの検証を行う。
  2. **削除時の安全なQuiesce / Shutdown**:
     - Machine削除時にTalos APIへshutdownを要求し、停止確認が取れるまでclaimを維持する。
     - 停止確認後に `TartHost.spec.retainedFrom` を記録してclaimを解除する。
- **解消条件**:
  - Machine作成からTalos OSインストール、認証済みAPI起動、および削除時の安全停止・Retentionまでが一貫してreconcileされること。

### タスク5: Raw Patch 合成エンジンの実装
- **重要度**: 中
- **現状**: [`bootstrap/secret.go`](../../bootstrap/secret.go) は完全なconfigurationのみ受け入れる暫定実装（L52 TODO）。
- **実装内容**:
  1. **設定マージパイプライン**:
     - cluster secret bundleとCAPI/Tart contextからbase configurationを生成。
     - `configSecretRef` から読み出したユーザーのraw patchを適用。
     - Provider-owned invariant（ProviderID、cluster endpoint、machine roleなど）を上書き不可として検証・適用。
     - 競合がある場合は上書きせず `Ready=False`、`Reason=ConfigurationConflict` を設定。
- **解消条件**:
  - ユーザーが任意のTalos raw patchを `configSecretRef` 経由で安全に適用できること。

### タスク6: Hardware Discovery / Maintenance Boot連携
- **重要度**: 低〜中
- **現状**: 静的登録情報との照合のみ。
- **実装内容**:
  1. **動的インベントリ収集**:
     - maintenance Talos bootしたHostから、MAC、System UUID、CPUアーキテクチャ、Disk詳細（WWID, Serial, Size, Model）を収集し、`TartHost.status.inventory` に反映する。
- **解消条件**:
  - 事前のハードウェア詳細調査なしにHost登録とインベントリ収集が自動で行われること。

### タスク7: Power Backend拡張 (Redfish/BMC)
- **重要度**: 低
- **現状**: Wake-on-LANのみ実装（[`boot/wakeonlan.go`](../../boot/wakeonlan.go)）。
- **実装内容**:
  1. **Redfish API連携**:
     - [`boot/power.go`](../../boot/power.go) の `PowerOn` インターフェースを満たすRedfish実装を追加する。
     - out-of-bandでの電源ON/OFFおよび電源状態の観測を可能にする。
- **解消条件**:
  - BMC経由での確実な電源管理と電源OFF確認ができること。
