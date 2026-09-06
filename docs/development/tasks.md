# 実装タスク一覧（未実装・仮実装機能）

この文書は、Tartにおいて現在未実装または仮実装（スタブや`NotImplemented`による安全停止）となっている機能の実装タスク、要件、設計上の注意点、解消条件をまとめた正本である。

すでに実装済みの型定義や基本ロジック（`TartHost`のatomic CAS claim、`TartCluster.spec.id`生成、初期bundle生成、完全構成SecretからのBootstrap Secret生成など）はコード（[`api/`](../../api)、[`host/`](../../host)、[`controller/`](../../controller)等）を参照すること。

---

## 実装状況サマリ

| 機能エリア | 現状 | 該当コード | 主な未実装内容 |
| --- | --- | --- | --- |
| **Runtime Extension (Update)** | Talos image変更とmachine configuration変更のin-place update、cordon/drain連携を実装 | [`extensions/handlers.go`](../../extensions/handlers.go), [`extensions/configuration.go`](../../extensions/configuration.go), [`update/`](../../update) | 実機でのreboot/回復シーケンス検証 |
| **Control Plane Reconcile** | 初回経路、Failure Domain分散、quorum-safe scale-down、CA rotationステートマシン実装済み | [`controller/tartcontrolplane_controller.go`](../../controller/tartcontrolplane_controller.go) | 実機でのcertificate有効期限・同時cutoverの検証 |
| **Cluster Reconcile** | 初期bundle経路、Failure Domain観測・反映、Control Plane Availableを集約したReady判定を実装 | [`controller/tartcluster_controller.go`](../../controller/tartcluster_controller.go) | なし |
| **Machine / Talos Reconcile** | 初回Install、shutdown/retention、Update Extension接続、Reprovision（recovery identityによるTalos Reset連携）を実装 | [`controller/tartmachine_controller.go`](../../controller/tartmachine_controller.go), [`controller/reprovision.go`](../../controller/reprovision.go), [`recovery/`](../../recovery), [`talos/client.go`](../../talos/client.go) | deletion時のCAPI drain連携、停止観測とReprovisionの実機検証 |
| **Raw Patch 合成** | 初期経路とUpdate Extensionへの接続、destructive差分判定を実装 | [`bootstrap/generate.go`](../../bootstrap/generate.go), [`controller/tartbootstrapconfig_controller.go`](../../controller/tartbootstrapconfig_controller.go), [`update/configuration.go`](../../update/configuration.go) | 実機でのpatch適用検証 |
| **Hardware Discovery** | 実装済み（初期観測、boot attempt履歴、disk identity重複時のallocation/apply停止） | [`controller/tarthost_controller.go`](../../controller/tarthost_controller.go), [`talos/client.go`](../../talos/client.go), [`host/identity.go`](../../host/identity.go) | なし |
| **Power Backend** | RedfishとWoLを実装 | [`boot/`](../../boot), [`controller/power.go`](../../controller/power.go) | vendorごとの実機差異とE2E確認 |

---

## タスク詳細

### タスク1: Runtime Extension (Update Extension) の実装

- **重要度**: 高
- **現状**: 実装済み。Talos image（version、schematic）変更に加えて、TartBootstrapConfigのraw patch差し替えによって生じるeffective machine configuration差分もin-place updateとして適用する。
- **設計の前提**: in-place updateとreboot-free updateは別概念である。rebootが必要であっても、同一CAPI Machine、同一TartMachine、同一TartHost、同一local storageのまま「configuration apply → controlled reboot → health recovery」で完結するならそれは完全なin-place updateであり、Machine replacementへは決してfallbackしない。Talos machine configの全fieldを独自にsafe/unsafeへ分類する巨大allowlistは持たず、ユーザーが明示するupdate policyと「data、identityを破壊するか」という粗いsafety boundaryだけで判断する。
- **実装内容**:
  1. **Update Policy**（[`api/bootstrap/v1alpha1/tartbootstrapconfig_types.go`](../../api/bootstrap/v1alpha1/tartbootstrapconfig_types.go)）:
     - `TartBootstrapConfig.spec.updatePolicy.configuration`が`Auto`（既定）、`Live`、`Reboot`、`InitialOnly`を表す。`TartBootstrapConfigTemplate`にも同じfieldがあり、TartControlPlaneが生成するTartBootstrapConfigへ伝播する。
     - `Auto`はTalos 1.14時点でreboot要否を信頼できる形で判定できないため`Reboot`として扱う。楽観的なreboot-free applyは行わない。この判定は[`update/policy.go`](../../update/policy.go)の`autoResolvesToReboot`の1箇所だけに存在し、将来Talosが信頼できる判定APIを提供した場合はここだけを変更する。
     - `Live`はユーザーがlive applyを明示したadvanced optionであり、`ApplyConfigurationRequest_NO_REBOOT`で適用する。失敗時にRebootへ自動fallbackせず、明示的な`Failure`で停止する。
     - `InitialOnly`は初回provisioning後の変更を許さず、差分検出時に`ReprovisionRequired`として安全停止する。Bootstrap Secretの作り直しも行わない。
  2. **Destructive判定**（[`update/configuration.go`](../../update/configuration.go)）:
     - Talos machineryのtyped configuration（`github.com/siderolabs/talos/pkg/machinery/config`）でactive configurationとdesired configurationを読み込み、install disk選択、install wipe設定、volume/LVM/RAID/swap等のdocument変更を`ReprovisionRequired`として除外する。
     - configuration documentは「data、identityを破壊しないと確証できるkind」のallowlistで判定し、列挙されていないkind（将来Talosへ追加される未知のkindを含む）は安全側としてdestructive扱いにする。
     - cluster identity/token、machine PKI/token、etcd PKI、Kubernetes PKI、cluster name、control-plane endpoint、machine role、Kubernetes component image、ProviderIDの競合は`InvariantConflict`として`Failure`で停止する。installer image identityの差分はTalos image upgrade pathが所有するため、configuration差分の判定からは正規化して除外する。
  3. **完全パッチ生成**:
     - `CanUpdateMachine` / `CanUpdateMachineSet`は、TartMachineのimage差分に加えてTartBootstrapConfigの`configPatchesSecretRef`と`updatePolicy`の差分を許可し、CAPI Machine / MachineSetへ完全なJSON patchを返す。`InitialOnly` policyでのraw patch変更、解釈できないpolicy、その他の差分はpatchなしの`Failure`でvetoする。
  4. **`UpdateMachine`の実行**（[`extensions/configuration.go`](../../extensions/configuration.go)）:
     - imageがdesiredへ到達した後、[`talos.Client.ActiveMachineConfiguration`](../../talos/client.go)で観測したactive configurationとimmutable Bootstrap Secretのdesired configurationを比較し、policyに従って適用する。
     - `Live`は`ApplyConfigurationLive`（NO_REBOOT）で適用する。`Auto`/`Reboot`は、control-planeのetcd quorum判定（`controlPlaneUpgradeSafe`）とcordon/drain（[`extensions/drain.go`](../../extensions/drain.go)の`enforceDrainPolicy`、`TartCluster.spec.updatePolicy.disruptionPolicy`）を満たしてからapplyし、Tart自身がTalos `Reboot` RPCでrebootをorchestrateする。
     - Update用にcordonしたNodeにはannotationで印を付け、update完了を確認できた時点でそのNodeだけをuncordonする。
  5. **Apply後の検証**:
     - 「RPCが成功した」ことを完了条件にしない。Talos APIの到達性、desired machine configurationの反映、rebootを伴う場合はboot時刻（`SystemStat`）の変化、Talos serviceのhealth、workload cluster上のNode Readyを観測してから`UpdateMachine`を完了させる。
     - Statusにはprogram counterやstep番号を保存せず、呼び出しごとにTalosとworkload clusterの観測から状態を再計算するため、controller再起動後も継続できる。
- **残作業（実機でのみ検証可能）**: 実際のreboot所要時間とPDBを持つworkloadでのrolling reboot、Live applyがTalos側で拒否されるケースの実挙動、control-planeでのquorum維持。
- **解消条件**: 達成済み。in-place update可能な変更に対して`Success`とpatchが返り、`UpdateMachine`がTalos APIの呼び出しと完了確認を行うこと。不安全な変更に対してCAPIがreplacementへfallbackせず安全停止すること。

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
  2. **Update Extensionへの接続（実装済み）**:
     - raw patch差し替えによって生じるeffective configuration差分は、[`update.Evaluate`](../../update/configuration.go)が`None` / `Updatable` / `ReprovisionRequired` / `InvariantConflict`へ分類し、`Updatable`のときだけ`TartBootstrapConfig.spec.updatePolicy.configuration`に従ってin-placeで適用する（詳細は[タスク1](#タスク1-runtime-extension-update-extension-の実装)）。
     - Bootstrap Secretはimmutableであるため、update policyが変更を許す場合だけ同じ名前で作り直し、desired configurationをUpdate Extensionから観測できるようにする。`InitialOnly` policyでは従来どおり`BootstrapSecretImmutable`として安全停止する。
- **解消条件**: 達成済み。ユーザーが任意のTalos raw patchを`configPatchesSecretRef`経由で安全に適用でき、破壊的な差分は`ReprovisionRequired`として停止すること。

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
- **状態**: 実装済み(`talos/kubernetes_upgrade.go`、`controller/kubernetesupgrade.go`、`controller/tartcontrolplane_controller.go`、`extensions/handlers.go`、`extensions/drain.go`)。
- **設計方針**: Talosが既に実装しているlifecycle logicは再実装しない。ただし必要な機能がTalosの公開RPCだけでは表現できない場合、Talos upstreamの既存Go実装を直接利用する。`talosctl upgrade-k8s`が呼ぶ`github.com/siderolabs/talos/pkg/cluster/kubernetes`が正本の実装であり、Tartはこれをコピーせずそのまま呼び出す。詳細は[設計判断](decisions.md)の「Talos upstream実装の直接利用とadapter隔離」を参照する。
- **実装内容**:
  1. **upstream adapter**: `talos/kubernetes_upgrade.go`が`talos.KubernetesUpgradeRunner` interface(`DetectVersion`、`Upgrade`)を定義し、`UpstreamKubernetesUpgradeRunner`がupstreamの`k8s.DetectLowestVersion`と`k8s.Upgrade`へ委譲する。upstreamが要求する`UpgradeProvider`(`cluster.ConfigClientProvider`と`cluster.KubernetesClient`)はTartのauthenticated Talos clientから組み立てる。upgrade path、component順序、health待ち、version skew検証はすべてupstream実装の責務である。`go.mod`では`github.com/siderolabs/talos`を`github.com/siderolabs/talos/pkg/machinery`と同じv1.14.0へpinする。
  2. **所有権**: cluster-wide Kubernetes upgradeはTartControlPlaneだけが実行する。`controller/kubernetesupgrade.go`の`reconcileKubernetesUpgrade`が`spec.version`と観測versionの差分を検出し、adapter経由でupgradeを一度だけ要求する。
  3. **preflight**: `evaluateKubernetesUpgrade`が、desired versionの妥当性、control plane初期化、workload Kubernetes APIのready、Talos API到達性、etcd health/quorum、全control-plane MachineのReady、他のlifecycle operation(CA rotation、scale-down)の非進行を判定する。満たさない場合はupgradeへ進まず待機し、desired versionが空の場合は`Reason=InvalidVersion`で安全停止する。判定は外部依存のない純粋関数である。
  4. **同時実行の禁止**: `coordination.k8s.io/v1 Lease`(`tart-kubernetes-upgrade-<control plane name>`)をresourceVersion CAS(Create/Update conflict)で取得し、同一clusterのupgradeを直列化する。controller replicaが複数存在しても高々1つだけが実行する。実行中はleaseをrenewし続ける。Leaseはcacheせず常にAPI serverから読む(`cmd/controller-manager/main.go`のClient CacheOptions)。
  5. **crash recovery**: Statusに保持するのは`status.kubernetesUpgrade`(target version、観測version、失敗理由)と`KubernetesUpgrading` Conditionだけであり、upgrade手順のstepやprogram counterを永続化しない。controllerが停止するとlease renewも止まり、lease duration経過後に他replicaが引き継ぐ。再開時は現在のcluster versionを観測し直して同じdesired versionへのupgradeを再要求し、完了済みstepの検出はupstream実装へ委ねる。
  6. **CAPI integration**: desired versionのsource of truthは`TartControlPlane.spec.version`の一本であり、Talos側で独立したversion stateを持たない。`UpToDate` Conditionは、Machineのstatusに加えてcluster Kubernetes versionがdesired versionへ収束していることを条件とする。
  7. **worker側**: `extensions/handlers.go`はKubernetes version差分に対して`Machine.spec.version`の伝播だけを許可し、`upgrade-k8s`相当の処理を実行しない。`updateMachineAtTalos`はimageとmachine configurationの収束後に、`nodeKubernetesVersionConverged`で自Nodeのkubelet versionがdesired versionへ収束したことだけを確認して完了する。
- **テスト**: preflight判定、lease取得可否、複数replicaでの排他、preflight未達時にupgradeを開始しないこと、収束済みclusterでupgradeを再実行しないことを`controller/kubernetesupgrade_test.go`で検証する。実際のTalos APIとworkload clusterを要するupgradeの実行はE2E/実機検証へ委ねる。
- **未検証事項**: 実機またはVMでのcluster-wide upgrade、Topology managed clusterでのupgrade開始条件とworkerへの伝播は未検証であり、[検証方針](verification.md)のE2Eで確認する。

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
- **現状**: **実装済み**。「Machineの寿命とHost上のTalos installationの寿命は異なる」という設計変更により、Machine/TartBootstrapConfigのownership lifecycleから独立した寿命を持つrecovery identityを導入し、Reprovision flow全体を実装した。
- **実装内容**:
  1. **Cluster Recovery Secret**（[`recovery/secret.go`](../../recovery/secret.go)）:
     - provider管理namespace上のimmutable Secret（`tart-talos-recovery-<cluster-id>-<ca-fingerprint>`）。`os-ca.crt`、`os-ca.key`、`clusterID`のみを保持する。
     - 「Talos cluster identity → recovery Secret → 複数のTartHost」という構造であり、Hostごとにcluster secret bundleやCA private keyを複製しない。Kubernetes PKIやBootstrap Data全体も複製しない。
     - 長寿命のadmin client certificateは保存せず、`Material.ClientCertificate`が必要時に短命（既定10分）な`os:admin` client certificateを都度発行する。Talos machine APIのReset RPCは`os:admin` roleを要求するため、この最小権限を都度発行する方式とした。
     - 生成元は[`controlplane/bundle.go`](../../controlplane/bundle.go)が管理するcluster secret bundleのactive generationであり、CA rotation機構と同じ`secrets.Bundle`構造を共有する。
  2. **確立のタイミング**（[`controller/reprovision.go`](../../controller/reprovision.go)の`ensureTalosIdentityBinding`）:
     - Machine削除の瞬間ではなく、Talos configurationをHostへapplyする前にrecovery Secretを作成し、`TartHost.status.currentTalosIdentityRef`へbindingを書く。
     - 既存bindingが別clusterを指す場合は上書きしない。同一clusterでCA rotationにより有効なCAが変わった場合だけ更新する（`shouldRebindTalosIdentity`）。
  3. **Lifetime管理**（[`controller/talosrecovery_controller.go`](../../controller/talosrecovery_controller.go)）:
     - `TartCluster`やMachineのOwnerReferenceでGCしない。定期reconcileで現在のTartHost集合を観測し、そのSecretを参照する`status.currentTalosIdentityRef`が1つも存在しない場合だけ削除する（[`recovery/retention.go`](../../recovery/retention.go)の`ShouldDelete`）。
     - 参照countのような壊れやすい状態を持たない。作成直後の短い猶予（`CreationGracePeriod`）でbinding書き込み前の削除を防ぐ。
  4. **Reprovision flow**（`reconcileReprovision`）:
     - `reconcileTalos`の分岐として、`reconcileAuthenticatedTalos`より前に実行する。
     - recovery Secret解決 → 短命証明書発行 → 旧Talos APIへ認証済み接続 → cluster identity検証 → host identity検証 → Reset → maintenance mode復帰確認 → binding解除 → 既存の`reconcileMaintenanceTalos`による通常のfresh provisioning → 新identityの再確立、という順序で進む。
     - Statusはstep番号ではなく観測結果とConditionsのみを保持し、controller再起動後も外部状態の再観測から安全に再開する。
  5. **Reset前のidentity verification**（[`recovery/identity.go`](../../recovery/identity.go)の`VerifyResetTarget`）:
     - TLS認証（recovery CAによるserver certificate検証とclient certificate提示）の成功を前提に、active machine configurationから観測したcluster ID、inventoryのMAC address、system UUID、接続endpointをすべて照合する。
     - MAC addressだけ、IP addressだけを根拠にせず、いずれか1つでも一致しない場合はResetを実行せずrequeueもせずに安全停止する。
  6. **Reset scope**: [`talos.Client.Reset`](../../talos/client.go)は`WipeMode=ALL`を明示し、`SystemPartitionsToWipe`と`UserDisksToWipe`は指定しない。system Talos installationのresetのみを意味し、別disk上のLonghorn/TopoLVM/raw volumeのデータは対象外である。詳細は[`docs/development/lifecycle.md`](lifecycle.md)の「Reset Scope」を参照する。
  7. **Reusable Hostのclaim経路**: 自動選択はAvailable Hostのみを対象とし、Reusable Hostは`TartMachine.spec.hostRef`による明示的な指定でだけclaimできる。
- **テスト**: Talos APIとのやりとりは`controller.TalosDialer` / `controller.TalosNode` interfaceで抽象化した。Wrong-host protection（別のMAC、別のsystem UUID、別のcluster、configuration観測失敗、reset後のmaintenance identity不一致）でResetが実行されないことと、Retain→承認→Reset→binding解除→通常経路復帰の流れを、Talos clientをfakeへ差し替えたGo testで検証している（[`controller/reprovision_test.go`](../../controller/reprovision_test.go)）。実機/VMを伴うE2Eは[検証方針](verification.md)の責務分離に従い別途実施する。
- **解消条件**: 満たしている。
