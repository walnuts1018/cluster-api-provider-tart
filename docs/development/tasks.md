# 実装タスク一覧（未実装・仮実装機能）

この文書は、Tartにおいて現在未実装または仮実装（スタブや`NotImplemented`による安全停止）となっている機能の実装タスク、要件、設計上の注意点、解消条件をまとめた正本である。

すでに実装済みの型定義や基本ロジック（`TartHost`のatomic CAS claim、`TartCluster.spec.id`生成、初期bundle生成、完全構成SecretからのBootstrap Secret生成など）はコード（[`api/`](../../api)、[`host/`](../../host)、[`controller/`](../../controller)等）を参照すること。

---

## 実装状況サマリ

| 機能エリア | 現状 | 該当コード | 主な未実装内容 |
| --- | --- | --- | --- |
| **Runtime Extension (Update)** | Talos image変更を実装 | [`extensions/handlers.go`](../../extensions/handlers.go) | machine configuration変更、drain連携、MachineSet実機適用の拡張 |
| **Control Plane Reconcile** | 初回経路、Failure Domain分散、quorum-safe scale-down、CA rotationステートマシン実装済み | [`controller/tartcontrolplane_controller.go`](../../controller/tartcontrolplane_controller.go) | 実機でのcertificate有効期限・同時cutoverの検証 |
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
     - effective configurationの差分評価と破壊的でないmachine configuration変更の許可は、Talos configuration read/apply APIの契約を追加した後に拡張する。稼働中nodeから現在のmachine configurationを読み出すAPI（[`talos.Client.ActiveMachineConfiguration`](../../talos/client.go)、CA rotationステートマシン実装のために追加済み）は利用可能になったが、`extensions/handlers.go`側のeffective configuration差分評価・許可ロジックへの接続は未着手であり、引き続き本タスクの残作業とする。
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
- **現状**: [`controller/tartcontrolplane_controller.go`](../../controller/tartcontrolplane_controller.go) はdesired replicasに基づく子リソース作成、初回etcd Bootstrap RPC呼び出し、workload kubeconfig生成、etcd/API readiness観測、pre-terminate hookを使ったquorum-safe scale-down、およびCA Rotationステートマシンまで実装済み。
- **実装内容**:
  1. **初回etcd Bootstrap RPC**:
     - 最初のcontrol-plane Machineが起動しmaintenanceから認証済みAPIへ移行した際、Talos `Bootstrap` RPCを一度だけ実行する。
     - API serverが受付可能になった時点で `controlPlaneInitialized` を `True` に設定する。
  2. **Workload kubeconfig Secretの生成と管理**:
     - `<cluster-name>-kubeconfig` Secretを生成し、client certificateの期限監視と更新を行う。
  3. **etcd Quorum監視と安全なScale Down**:
     - scale-down対象Machineの削除時に、pre-terminate delete hook（`pre-terminate.delete.hook.machine.cluster.x-k8s.io/...`）でetcd member removalとquorum維持を確認してから削除を許可する。
  4. **CA Rotationステートマシン（実装済み）**:
     - `TartCluster.spec.caRotationRequestedGeneration`が`status.activeSecretGeneration + 1`と一致すると、`TartClusterReconciler`が[`controlplane.GenerateRotatedBundleData`](../../controlplane/bundle.go)でmachine(OS)/Kubernetes API server/Kubernetes aggregatorのCAだけを新規生成した`Pending` bundle Secretを先行生成する（etcd CAとcluster/trustd tokenは維持し、既存clusterのmembershipやbootstrapへ影響しない）。
     - `TartControlPlaneReconciler.reconcileCARotation`が、各control-plane MachineへTalos認証接続し[`talos.Client.ActiveMachineConfiguration`](../../talos/client.go)で稼働中configurationを観測、[`controlplane.ObserveCATrustStage`](../../controlplane/rotation.go)でactive/pending bundleのCAとissuing/accepted CAを比較して進行段階（Stable→DualTrust→Cutover→Rotated）を判定する。Statusはこの観測結果をConditionとして表すだけで、step番号としては使わない。
     - 全machineの最小段階に応じて、[`talos.SetMachineCertificateAuthority`](../../talos/client.go)・`SetKubernetesAPICertificateAuthority`・`SetKubernetesAggregatorCertificateAuthority`で「accepted CA追加」→「issuing CA切替」→「旧CA削除」の順にTalos machine configurationをapplyする（Talosの`ApplyConfiguration`はAUTOモードのためcertificateの反映に再起動を要しない）。全machineがRotated段階に達すると、Pending bundle SecretのlabelをActiveへ書き換え、`TartCluster.status.activeSecretGeneration`を昇格させる。
     - controller再起動時も、Pending/Active bundle Secretの有無と各Machineから観測した実際のCA構成から進行段階を毎回再計算するため、途中状態をメモリやStatusのstep番号として保持しない。
     - **実機でのみ検証可能な残課題**: rotation中の実際のcertificate有効期限の遷移、複数control-plane Machineが同時にcutoverする際のetcd/API serverの実際の可用性、Talosバージョンごとの`ApplyConfiguration`の無停止反映可否、およびetcd CA自体のrotation（Talos機械configurationにaccepted-CAによる二重信頼の仕組みがないため今回はスコープ外とし、必要になった場合はetcd member単位の再起動を伴う別手順として設計する）。
- **解消条件**:
  - `TartControlPlaneAvailableCondition`, `TartControlPlaneEtcdClusterAvailableCondition` 等が実際の観測に基づいて正常に更新されること。
  - CA Rotationについては、`TartControlPlaneCARotatingCondition`が観測結果に基づいて進行・完了を反映し、実機での証明書有効期限とetcd/API可用性の検証が残ること。

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
- **現状**: maintenance Talos APIからMAC、system UUID、architecture、NIC、disk情報を取得して`TartHost.status.inventory`へ反映する初期観測を実装済み。まっさらなhostをTalos maintenance modeへ到達させるためのProxyDHCP/TFTP/iPXEスクリプト配信サーバーを`netboot/`パッケージと独立バイナリ`cmd/netboot-server`として実装済み（controller-managerとは別Deploymentで動作し、Resource modelの中心には置かない）。`config/default`に組み込み済みのため`clusterctl init --infrastructure tart`や`InfrastructureProvider`(cluster-api-operator)でのインストールだけでnetboot-serverも一緒にデプロイされる。
- **実装内容**:
  1. **動的インベントリ収集**:
     - maintenance Talos bootしたHostから、MAC、System UUID、CPUアーキテクチャ、Disk詳細（WWID, Serial, Size, Model）を収集し、`TartHost.status.inventory` に反映する。
  2. **netboot-server（実装済み）**:
     - 既存DHCPサーバーと共存するProxyDHCPで、PXE optionを持つrequestにのみiPXEブートローダのTFTP配信先を応答する。
     - TFTPは初期iPXEブートローダ(`ipxe-x86_64.efi`/`ipxe-arm64.efi`)のみを配信し、kernel/initramfsの取得はiPXEが自らHTTP経由で行う。
     - iPXEはHTTPハンドラ(`netboot/httpboot.go`)が返すスクリプトから、PXEクライアントのMACアドレスに対応する`TartHost`/`TartMachine`をKubernetes APIからread-onlyで参照し(`netboot/resolver.go`)、そのdesired Talos version/schematicIDでTalos Image FactoryのPXE配信endpoint(`https://pxe.factory.talos.dev/pxe/<schematicID>/v<version>/metal-<arch>`)へ直接chainする。対応するTartHost/TartMachineが存在しないMAC(初回enrollment boot)は、operator設定のdiscovery用Talos version/schematicIDへfallbackする。netboot-server自身はSecretを読まずStatus/Conditionを書かないstatelessなread-onlyアダプターであり、maintenance mode到達後のconfiguration適用はcontroller-manager側の既存reconcileが担う。
  3. **clusterctl/cluster-api-operator向けrelease workflow（実装済み）**:
     - `.github/workflows/release.yaml`がタグpushをトリガーにcontroller-managerとnetboot-serverの両imageをビルド・push後、`scripts/release-manifests/run.sh`(`mise run release-manifests`)で`infrastructure-components.yaml`、`metadata.yaml`、`infrastructure-provider.yaml`を生成しGitHub Releaseへ添付する。
- **残タスク（スコープ外の実機検証）**:
  - 実機vendorごとのPXE firmware差異（Option 93の有無、iPXE User-Classの実装差）の検証。
  - 複数NICを持つnodeでのProxyDHCP応答経路の検証、および既存DHCPサーバーとの共存確認。
  - IPv6 PXE bootへの対応要否の判断。
- **解消条件**:
  - 事前のハードウェア詳細調査なしにHost登録とインベントリ収集が自動で行われること。
  - netboot-serverが実機のPXE firmwareからTalos maintenance modeまで実際に到達できることをE2Eで確認すること。

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
