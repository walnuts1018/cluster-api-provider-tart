# 実装タスク一覧（未実装・仮実装機能）

この文書は、Tartにおいて現在未実装または仮実装（スタブや`NotImplemented`による安全停止）となっている機能の実装タスク、要件、設計上の注意点、解消条件をまとめた正本である。

すでに実装済みの型定義や基本ロジック（`TartHost`のatomic CAS claim、`TartCluster.spec.id`生成、初期bundle生成、完全構成SecretからのBootstrap Secret生成など）はコード（[`api/`](../../api)、[`host/`](../../host)、[`controller/`](../../controller)等）を参照すること。

---

## 実装状況サマリ

| 機能エリア | 現状 | 該当コード | 主な未実装内容 |
| --- | --- | --- | --- |
| **Runtime Extension (Update)** | Talos image変更を実装 | [`extensions/handlers.go`](../../extensions/handlers.go) | machine configuration変更、drain連携、MachineSet実機適用の拡張 |
| **Control Plane Reconcile** | 初回経路、Failure Domain分散、quorum-safe scale-down実装済み | [`controller/tartcontrolplane_controller.go`](../../controller/tartcontrolplane_controller.go) | CA rotation |
| **Cluster Reconcile** | 初期bundle経路とFailure Domain観測・反映を実装 | [`controller/tartcluster_controller.go`](../../controller/tartcluster_controller.go) | Ready条件のControl PlaneおよびInfrastructure集約 |
| **Machine / Talos Reconcile** | 初回Install、shutdown/retention、Update Extension接続を実装 | [`controller/tartmachine_controller.go`](../../controller/tartmachine_controller.go), [`talos/client.go`](../../talos/client.go) | deletion時のCAPI drain連携、停止観測の実機検証 |
| **Raw Patch 合成** | 初期経路実装済み | [`bootstrap/generate.go`](../../bootstrap/generate.go), [`controller/tartbootstrapconfig_controller.go`](../../controller/tartbootstrapconfig_controller.go) | 完全な安全差分判定とUpdate Extensionへの接続 |
| **Hardware Discovery** | 実装済み（初期観測） | [`controller/tarthost_controller.go`](../../controller/tarthost_controller.go), [`talos/client.go`](../../talos/client.go) | 複数boot attemptの追跡やdisk identity重複時のallocation停止 |
| **Power Backend** | RedfishとWoLを実装 | [`boot/`](../../boot), [`controller/power.go`](../../controller/power.go) | vendorごとの実機差異とE2E確認 |

---

## タスク詳細

### タスク1: Runtime Extension (Update Extension) の実装

- **重要度**: 高
- **現状**: Talos image versionおよびschematic変更は、差分全体を検査して完全な`spec` patchを返し、`UpdateMachine`から現行Hostとimmutable Bootstrap Secretを再観測してTalos `Upgrade` APIへ委譲する。image以外の差分、Bootstrap設定差分、identity変更はpatchなしで安全停止する。
- **実装内容**:
  1. **safe-diff判定エンジン**:
     - `CanUpdateMachineSet` / `CanUpdateMachine`はTartMachineのimage versionとschematicだけを許可し、その他の差分をpatchなしの`Failure`で確実にvetoする（fail-closed）。
     - effective configurationの差分評価と破壊的でないmachine configuration変更の許可は、Talos configuration read/apply APIの契約を追加した後に拡張する。
  2. **完全パッチ生成**:
     - 安全と判定された場合、CAPI MachineSet / Machineに対する完全なJSON patchを返却する。
  3. **`UpdateMachine` の実行**:
     - 現行Host binding、Bootstrap Secret、Talos versionを再観測してTalos `Upgrade` APIを呼び出し、reboot後にdesired versionを確認する。
     - drainとcordonはCAPI Machine controllerの責務として重複実装せず、Availability policyとの連携はCAPI contractを確認して追加する。
- **解消条件**:
  - in-place update可能な変更に対して `Success` とpatchが返り、`UpdateMachine` でTalos API呼び出しと完了確認が行われること。
  - 不安全な変更に対してCAPIがreplacementにfallbackせず安全停止すること。

### タスク2: TartControlPlane の高度なReconcile実装

- **重要度**: 高
- **現状**: [`controller/tartcontrolplane_controller.go`](../../controller/tartcontrolplane_controller.go) はdesired replicasに基づく子リソース作成、初回etcd Bootstrap RPC呼び出し、workload kubeconfig生成、etcd/API readiness観測、およびpre-terminate hookを使ったquorum-safe scale-downまで実装済み。CA rotationは未実装。
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
- **現状**: [`controller/tartcluster_controller.go`](../../controller/tartcluster_controller.go) では `spec.clusterID` 生成、初期bundle Secret生成、および登録済みHostのFailure Domain観測・反映を行っている。CAPI ClusterのInfrastructureReadyはこのResourceの`status.initialization.provisioned`と`Ready`を通じてCAPIへ委譲する。
- **実装内容**:
  1. **Cluster Ready判定**:
     - Control Planeおよび各Infrastructureコンポーネントの観測結果を集約し、Clusterの準備完了を判定してReady Conditionを更新する。
  2. **Failure Domainの観測と伝播**（実装済み）:
     - `TartHost`のFailure Domain設定を全件観測して重複排除・ソートし、`TartCluster.status.failureDomains`へ反映する。
     - CAPI Machineの`spec.failureDomain`をHost allocatorへ渡し、明示HostRefと自動選択の双方で一致しないHostをclaimしない。
- **解消条件**:
  - Cluster全体の健全性およびInfrastructure readinessがConditionへ正しく反映されること。

### タスク4: TartMachine のTalosライフサイクル完了処理

- **重要度**: 高
- **現状**: [`controller/tartmachine_controller.go`](../../controller/tartmachine_controller.go) はHost claim、ProviderID同期、maintenance APIのidentity照合、Talos configuration apply、再起動後のauthenticated API復帰とversion確認、削除時のshutdown確認とHost retentionを実装している。[`talos/client.go`](../../talos/client.go)はOS upgradeをTalos APIへ委譲する。
- **実装内容**:
  1. **Talos Install & Reboot管理**:
     - maintenance modeで起動したHostに対し、Bootstrap Secretから取得したconfigurationをapplyし、installを実行する。
     - reboot後の認証済みAPI到達とversion/schematicの検証を行う。
  2. **削除時の安全なQuiesce / Shutdown**:
     - Machine削除時に認証済みまたはidentity検証済みmaintenance Talos APIへshutdownを要求し、API停止確認が取れるまでclaimを維持する。
     - 停止確認後に`TartHost.spec.previousConsumerRef`をatomicに記録してclaimを解除し、Machine finalizerを外す。
- **解消条件**:
  - Machine作成からTalos OSインストール、認証済みAPI起動、および削除時の安全停止・Retentionまでが一貫してreconcileされること。

### タスク5: Raw Patch 合成エンジンの実装

- **重要度**: 中
- **現状**: `patches` keyを持つimmutable Secretについて、Talos machineryの生成base、active bundle、CAPI Machine contextへraw patchを適用し、cluster名、Endpoint、Machine Role、machine/cluster/Kubernetes PKI、token、Kubernetes component imageのprovider-owned invariantを検証する経路を実装済み。`value` keyの完全configuration入力も同じinvariant検証を通過した場合だけ利用できる。
- **実装内容**:
  1. **設定マージパイプライン**（基本経路実装済み）:
     - cluster secret bundleとCAPI/Tart contextからbase configurationを生成。
     - `configSecretRef` から読み出したユーザーのraw patchを適用。
     - machine role、cluster endpoint、Kubernetes versionをTalos machineryのbaseへ反映。
  2. **残タスク**:
     - Update Extensionで利用するeffective configurationの完全な安全差分判定。
- **解消条件**:
  - ユーザーが任意のTalos raw patchを `configSecretRef` 経由で安全に適用できること。

### タスク6: Hardware Discovery / Maintenance Boot連携

- **重要度**: 低〜中
- **現状**: maintenance Talos APIからMAC、system UUID、architecture、NIC、disk情報を取得して`TartHost.status.inventory`へ反映する初期観測を実装済み。
- **実装内容**:
  1. **動的インベントリ収集**:
     - maintenance Talos bootしたHostから、MAC、System UUID、CPUアーキテクチャ、Disk詳細（WWID, Serial, Size, Model）を収集し、`TartHost.status.inventory` に反映する。
- **解消条件**:
  - 事前のハードウェア詳細調査なしにHost登録とインベントリ収集が自動で行われること。

### タスク7: Power Backend拡張 (Redfish/BMC)

- **重要度**: 低
- **現状**: RedfishとWake-on-LANを実装済み。Redfishは固定のprovider管理namespaceからcredentialおよびcustom CA Secretを解決し、Service Root、Systems collection、ComputerSystemの`PowerState`とReset actionだけを利用する。初回Discoveryでは必要な場合だけ電源を投入し、Machine削除ではTalos `Shutdown`後に`PowerState=Off`を確認する。
- **実装内容**:
  1. **Redfish API連携**:
     - [`boot/power.go`](../../boot/power.go) の`PowerOn`、`PowerOff`、`PowerStateObserver`契約を満たすRedfish実装を追加する。
     - out-of-bandでの電源ON/OFFおよび電源状態の観測を可能にする。
     - Systemが複数存在する場合は`systemID`を必須とし、設定されたendpoint外のRedfish linkを拒否する。
     - 秘密情報の取得、TLS設定、controllerの停止確認連携を追加する。
- **解消条件**:
  - BMC経由での電源管理と電源OFF確認ができること。実機vendor差異、TLS、credential rotation、電源断の受入れ確認はE2Eで別途実施する。
