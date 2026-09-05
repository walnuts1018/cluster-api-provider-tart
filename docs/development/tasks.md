# 実装タスク一覧（未実装・仮実装機能）

この文書は、Tartにおいて現在未実装または仮実装（スタブや`NotImplemented`による安全停止）となっている機能の実装タスク、要件、設計上の注意点、解消条件をまとめた正本である。

すでに実装済みの型定義や基本ロジック（`TartHost`のatomic CAS claim、`TartCluster.spec.id`生成、初期bundle生成、完全構成SecretからのBootstrap Secret生成など）はコード（[`api/`](../../api)、[`host/`](../../host)、[`controller/`](../../controller)等）を参照すること。

---

## 実装状況サマリ

| 機能エリア | 現状 | 該当コード | 主な未実装内容 |
| --- | --- | --- | --- |
| **Runtime Extension (Update)** | Talos image変更を実装 | [`extensions/handlers.go`](../../extensions/handlers.go) | machine configuration変更、drain連携、MachineSet実機適用の拡張 |
| **Control Plane Reconcile** | 初回経路、Failure Domain分散、quorum-safe scale-down、CA rotationステートマシン実装済み | [`controller/tartcontrolplane_controller.go`](../../controller/tartcontrolplane_controller.go) | 実機でのcertificate有効期限・同時cutoverの検証 |
| **Cluster Reconcile** | 初期bundle経路、Failure Domain観測・反映、Control Plane Availableを集約したReady判定を実装 | [`controller/tartcluster_controller.go`](../../controller/tartcluster_controller.go) | なし |
| **Machine / Talos Reconcile** | 初回Install、shutdown/retention、Update Extension接続を実装 | [`controller/tartmachine_controller.go`](../../controller/tartmachine_controller.go), [`talos/client.go`](../../talos/client.go) | deletion時のCAPI drain連携、停止観測の実機検証 |
| **Raw Patch 合成** | 初期経路実装済み | [`bootstrap/generate.go`](../../bootstrap/generate.go), [`controller/tartbootstrapconfig_controller.go`](../../controller/tartbootstrapconfig_controller.go) | 完全な安全差分判定とUpdate Extensionへの接続 |
| **Hardware Discovery** | 実装済み（初期観測、boot attempt履歴、disk identity重複時のallocation/apply停止） | [`controller/tarthost_controller.go`](../../controller/tarthost_controller.go), [`talos/client.go`](../../talos/client.go), [`host/identity.go`](../../host/identity.go) | なし |
| **Power Backend** | RedfishとWoLを実装 | [`boot/`](../../boot), [`controller/power.go`](../../controller/power.go) | vendorごとの実機差異とE2E確認 |

---

## タスク詳細

### タスク1: Runtime Extension (Update Extension) の実装

- **重要度**: 高
- **現状**: Talos image versionおよびschematic変更は、差分全体を検査して完全な`spec` patchを返し、`UpdateMachine`から現行Hostとimmutable Bootstrap Secretを再観測してTalos `Upgrade` APIへ委譲する。image以外の差分、Bootstrap設定差分、identity変更はpatchなしで安全停止する。
- **実装内容**:
  1. **safe-diff判定エンジン**:
     - `CanUpdateMachineSet` / `CanUpdateMachine`はTartMachineのimage versionとschematicだけを許可し、その他の差分をpatchなしの`Failure`で確実にvetoする（fail-closed）。
     - effective configurationの差分評価と破壊的でないmachine configuration変更の許可は、Talos configuration read/apply APIの契約を追加した後に拡張する予定だったが、以下の調査により**現時点では安全に実装できないと判断し、着手を見送った**。稼働中nodeから現在のmachine configurationを読み出すAPI（[`talos.Client.ActiveMachineConfiguration`](../../talos/client.go)、CA rotationステートマシン実装のために追加済み）自体は利用可能である。
     - **調査結果と見送りの理由**: Talos machine configurationのfieldごとに「reboot・disk再構成・ネットワーク瞬断なしに安全にapplyできるか」をTalos自身が判定して返す機構があるか確認するため、`github.com/siderolabs/talos/pkg/machinery`が依存するTalos本体v1.14.0の`internal/app/machined/internal/server/v1alpha1/v1alpha1_server.go`の`Server.ApplyConfiguration`実装を読んだ。`ApplyConfigurationRequest_AUTO`モードは、on-diskのfield単位の安全性を判定することなく`ApplyConfigurationRequest_NO_REBOOT`へ無条件で読み替えられるだけであり（`switch in.Mode { case machine.ApplyConfigurationRequest_AUTO: in.Mode = machine.ApplyConfigurationRequest_NO_REBOOT; ... }`）、応答の`ModeDetails`も"Applied configuration without a reboot"という固定文言を返すだけで、実際にその差分がreboot不要で安全に反映されたかどうかを検証・保証しない。つまりTalos側に「この差分は安全」という信頼できる判定根拠が存在しないため、`talos.Client`にconfiguration apply APIを追加してこの応答を安全性判定の根拠に使うことはできない。
     - Talos machine configurationの各fieldを独自に「安全」「危険」へ分類する案も検討したが、Talos側の裏付けなしに「たぶん安全」なfieldをこちらだけの判断で許可することはfail-closedの原則に反するため採用しない。
     - **残作業**: Talosの将来versionでfield単位のreboot要否判定APIが追加された場合、または個々のconfiguration documentの反映がreboot-freeであることをTalos公式ドキュメント/ソースから確証できるfield（例: 特定のバージョンのKubeletConfig等）が明確になった場合に、本タスクを再評価する。それまではmachine configuration差分の許可は行わず、Talos image versionとschematicIDの変更のみをin-place updateの対象とする現状を維持する。
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
- **現状**: [`controller/tartcluster_controller.go`](../../controller/tartcluster_controller.go) では `spec.clusterID` 生成、初期bundle Secret生成、登録済みHostのFailure Domain観測・反映、およびControl Planeの健全性を集約したReady判定を行っている。CAPI ClusterのInfrastructureReadyはこのResourceの`status.initialization.provisioned`を通じてCAPIへ委譲する(`status.initialization.provisioned`はTartControlPlaneの子Machine作成の前提であるため、TartControlPlaneの準備完了を待たずsecret bundleの準備完了だけで`true`にする。循環依存を避けるため)。
- **実装内容**:
  1. **Cluster Ready判定（実装済み）**:
     - `aggregateReadiness`が、このClusterと同名の`clusterv1.ClusterNameLabel`を持つ`TartControlPlane`を検索し、その`Available` Conditionを観測する。TartControlPlaneがまだ存在しない場合(初回provisioning前)はsecret bundleの準備完了のみでReady=Trueとし、存在する場合はAvailableでない限りReady=Falseとする。TartControlPlaneの変更はWatchで即座にreconcileへ反映される。
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
     - Update Extensionで利用するeffective configurationの完全な安全差分判定。[タスク1](#タスク1-runtime-extension-update-extension-の実装)で調査した通り、Talos v1.14.0の`ApplyConfiguration`はAUTOモードをfield単位の安全性判定なしにNO_REBOOTへ読み替えるだけであり、Talos自身から信頼できる安全性判定根拠を得られなかったため着手を見送った。Talos側に判定機構が追加されるか、個別fieldのreboot-freeさを別途確証できるまで、raw patchの差分はUpdate Extensionでのin-place許可対象に含めない。
- **解消条件**:
  - ユーザーが任意のTalos raw patchを `configSecretRef` 経由で安全に適用できること。

### タスク6: Hardware Discovery / Maintenance Boot連携

- **重要度**: 低〜中
- **現状**: maintenance Talos APIからMAC、system UUID、architecture、NIC、disk情報を取得して`TartHost.status.inventory`へ反映する初期観測を実装済み。まっさらなhostをTalos maintenance modeへ到達させるためのProxyDHCP/TFTP/iPXEスクリプト配信サーバーを`netboot/`パッケージと独立バイナリ`cmd/netboot-server`として実装済み（controller-managerとは別Deploymentで動作し、Resource modelの中心には置かない）。`config/default`に組み込み済みのため`clusterctl init --infrastructure tart`や`InfrastructureProvider`(cluster-api-operator)でのインストールだけでnetboot-serverも一緒にデプロイされる。
- **実装内容**:
  1. **動的インベントリ収集**:
     - maintenance Talos bootしたHostから、MAC、System UUID、CPUアーキテクチャ、Disk詳細（WWID, Serial, Size, Model）を収集し、`TartHost.status.inventory` に反映する。
  2. **複数boot attemptの追跡（実装済み）**:
     - [`TartHost.status.bootAttempts`](../../api/infrastructure/v1alpha1/tarthost_types.go)へ、maintenance boot観測ごとの`bootID`、`firstObservedAt`/`lastObservedAt`、`systemUUID`、`endpoint`を直近16件までboundedに保持する([`controller/tarthost_controller.go`](../../controller/tarthost_controller.go)の`recordBootAttempt`)。同一`bootID`の再観測はタイムスタンプとsystemUUID/endpointを更新するだけで新規entryを追加しない。この履歴はidentity検証の追加材料であり、allocation可否の判定自体は常に現在の`status.inventory`から再計算する。
  3. **disk identity重複検出によるallocation/configuration apply停止（実装済み）**:
     - [`host.HasIdentityConflict`](../../host/identity.go)がMAC address、system UUID、disk identity(WWIDまたはserial、大文字小文字を無視して比較)の重複をcluster全体の`TartHost`横断で検出する。同一Host内で複数diskが同じWWID/serialを報告した場合(`diskIdentityConflictWithin`)も検出対象である。
     - `TartHostReconciler`は`HasIdentityConflictForAny`で重複を検知すると該当する全Hostを`Ready=False`にし、disk identityのみが原因の場合は`Reason=DiskIdentityConflict`、MAC/system UUIDを含む場合は`Reason=IdentityConflict`を設定した上でKubernetes Event(Warning、英語)を発行する。誤って一部Hostだけを除外すると誤ったHostへconfiguration applyしてしまうため、重複が解消するまで関係する全Hostを対象から外す。
     - `TartMachineReconciler`(host claim前)と`TartBootstrapConfigReconciler`(install disk選択前)も同じ`HasIdentityConflictForAny`判定を経由し、重複解消まで新規allocationとconfiguration生成の両方を安全停止する。
  4. **netboot-server（実装済み）**:
     - 既存DHCPサーバーと共存するProxyDHCPで、PXE optionを持つrequestにのみiPXEブートローダのTFTP配信先を応答する。
     - TFTPは初期iPXEブートローダ(`ipxe-x86_64.efi`/`ipxe-arm64.efi`)のみを配信し、kernel/initramfsの取得はiPXEが自らHTTP経由で行う。
     - iPXEはHTTPハンドラ(`netboot/httpboot.go`)が返すスクリプトから、PXEクライアントのMACアドレスに対応する`TartHost`/`TartMachine`をKubernetes APIからread-onlyで参照し(`netboot/resolver.go`)、そのdesired Talos version/schematicIDでTalos Image FactoryのPXE配信endpoint(`https://pxe.factory.talos.dev/pxe/<schematicID>/v<version>/metal-<arch>`)へ直接chainする。対応するTartHost/TartMachineが存在しないMAC(初回enrollment boot)は、operator設定のdiscovery用Talos version/schematicIDへfallbackする。netboot-server自身はSecretを読まずStatus/Conditionを書かないstatelessなread-onlyアダプターであり、maintenance mode到達後のconfiguration適用はcontroller-manager側の既存reconcileが担う。
  5. **clusterctl/cluster-api-operator向けrelease workflow（実装済み）**:
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

### タスク8: Kubernetes Upgrade (cluster-wide `upgrade-k8s`) の実装

- **重要度**: 高
- **現状**: [`docs/development/lifecycle.md`](lifecycle.md)の「Kubernetes Upgradeの収束規則（実装前の契約）」に定義済みの契約はあるが未実装。`TartControlPlane.spec.version`とCAPI Machineの`spec.version`(Kubernetes version)の差分は、Update Extension(`extensions/handlers.go`)がpatchなしで安全停止するだけであり、`talos upgrade-k8s`をcluster単位で一度だけ要求するorchestrationが存在しない。
- **調査結果 (見送り)**: `talosctl upgrade-k8s`に対応する単一のgRPC RPCはTalos machine APIに存在しない。`github.com/siderolabs/talos@v1.14.0`の`api/machine/machine.proto`にKubernetes upgrade関連のRPCはなく、`cmd/talosctl/cmd/talos/upgrade-k8s.go`が実際に呼んでいるのは`github.com/siderolabs/talos/pkg/cluster/kubernetes`パッケージの`k8s.Upgrade`関数である。この関数は以下を含む1,600行超のclient-side orchestrationであり、単一のRPC呼び出しに還元できない:
  - `k8s.DetectLowestVersion`によるcluster内の現在のKubernetes componentバージョン検出
  - control-planeの各Nodeへ、component(kube-apiserver/kube-controller-manager/kube-scheduler)ごとに個別のmachine configuration patchを順次apply(既存の`ApplyConfiguration`/`ActiveMachineConfiguration`は使えるが、component単位の安全な順序制御とhealth check gatingをこちらで再実装する必要がある)
  - kubeletのアップグレード
  - kube-proxy、CoreDNSなどのcore manifestを、Kubernetes API に対する Server-Side Apply(`github.com/siderolabs/go-kubernetes/kubernetes/ssa`)で直接更新
  - 各stepごとのreconcile/health待ち合わせ
  
  加えて、この実装(`pkg/cluster/kubernetes`)は`github.com/siderolabs/talos/pkg/machinery`とは別のGo module(`github.com/siderolabs/talos`本体、`go.mod`で確認済み)に属し、talosctl(CLI)向けの内部実装であって安定した外部向けライブラリ契約として提供されているものではない。取り込むには本体モジュール全体への依存追加が必要で、Talosのリリースサイクルに伴う破壊的変更を受けやすく、かつ「Talosのupgrade、Kubernetes runtimeを再実装しない」というProviderのスコープ制約に反する。
  - 安全に呼び出せる契約(単一RPC、または安定した公開ライブラリAPI)が存在しないため、本タスクの実装は**見送る**。Providerからcluster-wide `upgrade-k8s`を呼び出す安全な経路が確認できるまで、`extensions/handlers.go`はKubernetes version差分をpatchなしでvetoし続ける(fail-closedを維持)。
  - 再検討の条件: 将来のTalosリリースで`api/machine/machine.proto`にKubernetes upgrade用のRPCが追加される、または`pkg/cluster/kubernetes`相当の機能が`pkg/machinery`モジュール配下の安定APIとして公開された場合。
- **解消条件 (将来実装時)**:
  - `TartControlPlane.spec.version`の変更が、cluster-wide `upgrade-k8s`の一度だけの実行とKubernetes API/Node/control planeの完了観測を経て`status.versions`へ安全に反映されること。
  - version skewが不正な場合に`Ready=False`、`Reason=VersionSkew`で安全停止すること。
  - Topology managed clusterでの`upgrade-k8s`開始条件と、worker Machineへの伝播が矛盾なく動作すること(実機でのみ検証可能な部分はE2Eへ委ねる)。

### タスク9: node-disruptiveなin-place update前のcordon/drainと`allowDowntime`policyの実装

- **重要度**: 高
- **状態**: 実装済み(`extensions/drain.go`、`extensions/handlers.go`の`updateMachineAtTalos`)。
- **実装内容**:
  1. **Workload cluster Kubernetes clientの取得**: `extensions/drain.go`の`workloadClientForMachine`が、`controller/tartcontrolplane_controller.go`の`ensureKubeconfigSecret`が生成する`<cluster-name>-kubeconfig` Secret(`value` key)からclient-go clientsetを構築する。
  2. **対象NodeのCordon**: `findNodeByProviderID`が、更新対象TartMachineの`spec.providerID`と一致する`Node.spec.providerID`を持つworkload cluster Nodeを検索し、`cordonNode`が`Spec.Unschedulable = true`にする。
  3. **Drainとeviction**: `drainNode`がpolicy/v1 Eviction APIでNode上のPodをevictする。`podRequiresEviction`がDaemonSet管理下のPodとmirror Podを対象から除外する(kubectl drain相当のフィルタリング)。PDB起因のeviction拒否(429 Too Many Requests)は`drainOutcome.pdbBlockedOnly`として、それ以外の失敗と区別する。
  4. **`allowDowntime` policyの適用**: `enforceDrainPolicy`が、drain成功時はそのままUpgradeへ進め、PDB/availability起因の失敗時のみ`getUpdateTartCluster`(CAPI Cluster経由でTartClusterを取得)の`Spec.UpdatePolicy.DisruptionPolicy == DisruptionPolicyAllowDowntime`を確認してgraceful rebootを許容する。それ以外の失敗、またはpolicy未許可時は`setUpdateRetry`パターンで安全に中断する(`Ready=False`にはしない)。`updateMachineAtTalos`では、control planeの`controlPlaneUpgradeSafe`(etcd quorum)とcordon/drainの両方を独立に実施し、両方が通らない限りUpgradeへ進めない。
- **既知の制約・未検証事項**:
  - workload cluster Nodeが未観測(初回起動直後、kubeconfig Secret未発行等)の場合はcordon/drain対象が存在しないとみなしてそのまま進める設計。実機での動作は未検証。
  - `enforceDrainPolicy`の各種タイムアウトは`talosUpdateTimeout`(20秒)を共用しており、大規模クラスターでのdrain所要時間には未対応。必要であれば専用のtimeout/backoff設計を追加検討する。
  - テストコードは追加していない(ユーザー指示によりテスト追加は見送り)。実機・VMでの動作確認も未実施。
- **解消条件**:
  - `allowDowntime: false`(既定)において、drain失敗時に更新が安全に中断されること(verification.mdの受け入れ確認項目3)。
  - `allowDowntime: true`の場合のみ、availability/PDB/capacity起因のdrain失敗を許容してgraceful rebootが行われること。
  - 破壊的変更やquorum違反がこのpolicyで緩和されないこと。

### タスク10: Reusable Host「Reprovision」モードのTalos Reset連携

- **重要度**: 中
- **現状**: [`docs/development/lifecycle.md`](lifecycle.md)の「Reusableの2つの動作」節で、`Adopt`(既存installを保持したままclaim)と`Reprovision`(データ破棄を承認しTalosのreset/installer機構へ委譲してから新しいMachineへclaim)の2つのReuse modeを定義している。`Adopt`は`TartMachineReconciler.reconcileAuthenticatedTalos`が既存のBootstrap Secretで認証済みAPIへ接続を試み、成功すれば(=cryptographicにcluster secretが一致する既存installとして)そのまま`Provisioned`として扱うことで実質的に満たされている。しかし`Reprovision`は、`host/eligibility.go`のeligibility分類(`ReuseModeReprovision`)が存在するだけで、実際にTalosへreset RPCを発行して既存installを消去し、maintenance modeへ戻す処理が存在しない。`talos/client.go`にもReset APIがない。このため`spec.reuseMode: Reprovision`で承認されたHostをclaimしても、既存installが残ったままmaintenance APIが利用できず、Talos installへ進めずreconcileが停止し続ける。
- **実装内容**:
  1. **`talos.Client`へのReset API追加**:
     - Talos machine APIの`Reset`RPC(`github.com/siderolabs/talos/pkg/machinery/api/machine`。system disk wipe、graceful reboot into maintenance modeを指定できるオプションを確認する)を呼び出すメソッドを追加する。
     - Resetは対象Hostのデータを不可逆に消去する操作であるため、認証済みAPI(既存installのcluster secretで接続できる場合)またはRedfish/WoLでの物理電源operationとは独立して、`spec.reuseMode: Reprovision`かつ`spec.reuseApproval`が現在の`retainedFrom`と一致する場合にのみ呼び出せるようguardする。
  2. **`TartMachineReconciler`でのReprovisionオーケストレーション**:
     - claimしたHostが`Reprovision`モードで選択されていることを観測したら、install前に一度だけTalos `Reset`を要求し、maintenance modeへ戻ったことを観測してから通常のfresh install経路(`reconcileMaintenanceTalos`)へ合流させる。
     - Reset要求とその完了観測はProvider-owned invariantやHost claimのatomicityを壊さず、controller再起動時も外部観測(現在のTalos APIモード、`status`)から安全に再開できるようにする(Statusをprogram counterとして使わない)。
     - Reset失敗、または対象Hostの認証が現在のretainedFrom(旧cluster)と一致しない場合は、データ破壊を伴う操作であるため安全側に倒して`Ready=False`で停止する。
- **解消条件**:
  - `spec.reuseMode: Reprovision`で承認されたHostが、Talos Resetとmaintenance mode復帰を経て新しいTartMachineへ正常にinstallされること。
  - 承認や識別が一致しない場合にResetを実行せず安全停止すること。
