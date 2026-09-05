# Talos Linux導入

TartはTalos Linux専用のCluster API Providerです。Fresh machineの通常経路は、外部boot環境またはWake-on-LANでmaintenance Talosを起動でき、管理クラスタからそのendpointへ到達できる構成で利用できます。実機E2Eを実行できる環境がないため、release manifestとHA、workerの受け入れ保証はまだ提供していません。

## 導入モデル

管理クラスタへCAPI core、TartのInfrastructure Provider、Bootstrap Provider、Control Plane Providerを導入し、`TartHost`へHostのidentity、Talos maintenance API endpoint、power/boot capabilityを登録します。`TartBootstrapConfig.spec.configPatchesSecretRef`は省略でき、省略時はCAPIとTartのcluster contextからTalos configurationを生成します。patchを指定する場合はimmutableなSecretへ保存します。ユーザーはinstall前にLinux device pathやdisk UUIDを登録する必要はありません。in-place updateを使う場合はCAPIの`RuntimeSDK`と`InPlaceUpdates`を有効にし、TartのHTTPS endpointを`ExtensionConfig`へ登録します。

Redfishを使用する`TartHost`では、`spec.power.redfish.address`へRedfish Service Rootを指定し、provider管理namespaceのcredential Secretへ`username`と`password`を保存します。Systemsが複数ある場合は`systemID`を必ず指定し、必要に応じて同namespaceのCA Secretの`ca.crt`を参照します。credentialの値はStatus、Event、logへ出力されません。

```text
TartHost登録
    ↓
外部network bootまたはpower/boot backend
    ↓
Talos maintenance environment
    ↓
hardware discovery
    ↓
TartBootstrapConfigで生成したmachine configuration
    ↓
Talos installerとauthenticated Talos API
    ↓
CAPI Cluster Ready
```

OS installation、disk/volume、encryption、system extension、machine configuration、upgrade、rollback、Kubernetes runtimeはTalosへ委譲します。Cilium、Longhorn、TopoLVM、kube-vipなどはTalos configurationとKubernetes addon layerで構成し、Tart専用APIは使用しません。

初回installのdiskはmaintenance APIで観測した`TartHost.status.inventory`から自動選択され、生成したcomplete configurationへstable selectorとして組み込まれます。`TartMachine`はdesired installer imageを設定してmaintenance APIへ渡し、Talosのinstallerによるinstallとrebootの後、authenticated APIのversion観測で起動完了を確認します。

現在の組み込みcontroller(controller-manager)はDHCP、TFTP、PXEのendpoint discoveryを提供しないため、`TartHost.spec.talosAPIAddress`または外部boot連携が更新した`status.addresses`のいずれかが必要です。maintenance Talosをそのendpointで起動してからCAPI Machineを作成すると、inventory取得、disk選択、configuration apply、Talos installerによるreboot、authenticated APIの再接続までが通常経路で収束します。

### netboot-serverによるDiscovery boot

まっさらなhostがPXE bootでTalos maintenance modeまで自動的に到達できるようにするため、Infrastructure Provider(`config/default`)には`netboot-server`(実装は`netboot/`パッケージ、バイナリは`cmd/netboot-server`)がcontroller-managerとは別Deploymentとして同梱されており、`clusterctl init --infrastructure tart`や`InfrastructureProvider`(cluster-api-operator)でインストールしただけで一緒にデプロイされます。既存のDHCPサーバーと共存できるProxyDHCPとしてPXE option(Option 93)を持つrequestにのみ応答し、TFTPで初期iPXEブートローダを配信します。iPXE起動後はHTTP経由でiPXEスクリプトを取得します。

netboot-serverはTartHost/TartMachineをKubernetes APIからread-only(`get`/`list`/`watch`のみ、Secretへのアクセス権限なし)で参照し、PXEリクエストのMACアドレスに一致する`TartHost.spec.macAddress`とその`consumerRef`が指す`TartMachine.spec.image`から、そのHost向けのdesired Talos versionとschematicIDを解決してTalos Image FactoryのPXE配信endpoint(`https://pxe.factory.talos.dev/pxe/<schematicID>/v<version>/metal-<arch>`)へchainします。kernel/initramfsのURLはTart側で組み立てず、Image Factoryへ委譲します。netboot-server自身はSecretを読まず、Status/Conditionを書かないstatelessなread-onlyアダプターであり、再起動してもKubernetes APIの再watchだけで復旧します。

対応するTartHost/TartMachineがまだ存在しないMAC(Host登録前の初回enrollment boot)からのPXEリクエストは、operatorが設定したdiscovery用のTalos version/schematicIDへfallbackします。discovery imageは`netboot-server`Deploymentの環境変数`TART_NETBOOT_DISCOVERY_TALOS_VERSION`/`TART_NETBOOT_DISCOVERY_SCHEMATIC_ID`で設定します(未設定の場合、初回enrollment bootのhostはmaintenance modeへ到達できませんが、既に登録済みのHostのPXE bootはそのまま動作します)。`advertise-http-base-url`はbind addressから自動検出されるため、通常は設定不要ですが、複数NICやNAT環境では`TART_NETBOOT_ADVERTISE_HTTP_BASE_URL`で明示的に上書きしてください。

netboot-serverの実行には以下の点に注意してください。

- DHCP(port 67)、ProxyDHCP(port 4011)、TFTP(port 69)はwell-known/低番portであり、`CAP_NET_BIND_SERVICE`が必要です(Deploymentへ設定済み)。
- ProxyDHCPはbroadcastパケットを直接受け取る必要があるため、通常のPod networkでは動作しません。`hostNetwork: true`で動作させ、対象network segmentに到達可能なnodeへ配置してください(Deploymentへ設定済み)。

### リリースmanifestの取得

`clusterctl init`や`InfrastructureProvider`が参照するGitHub Releaseには、`infrastructure-components.yaml`(controller-managerとnetboot-serverの両方を含む)、`metadata.yaml`、`infrastructure-provider.yaml`(cluster-api-operator用のサンプルCR)がrelease workflow(`.github/workflows/release.yaml`)によって自動生成・添付されます。手元で同じ内容を生成する場合は`mise run release-manifests`(`CONTROLLER_IMAGE`、`NETBOOT_SERVER_IMAGE`環境変数が必要)を使ってください。
- `--discovery-talos-version`と`--discovery-schematic-id`には、discovery boot専用のTalos version/schematicIDを指定します。
- 複数NIC環境でのProxyDHCP応答経路や実機vendorごとのPXE firmware差異は、実機環境でのE2E検証がスコープ外として残っています（詳細は`docs/development/tasks.md`のタスク6を参照）。

未構成Talosのmaintenance APIはTLSで暗号化されますが認証済みではありません。machine configurationを送る前に`TartHost`、boot attempt、MAC/DHCP、endpoint、system UUID/inventoryを結び付け、曖昧なら停止します。installation後はauthenticated Talos APIへ移行します。Machine削除時はshutdownと停止確認後にclaimを解除し、Hostをdata保持の`Retained`として残します。

## 安全性

通常のTalos/Kubernetes updateはin-placeで実行し、CAPI Machine、`TartMachine`、`TartHost`、diskを維持します。Machine削除でもHostの物理dataは保持し、cleaning、reprovisioning、disk wipeは明示的な別操作とします。

初期boot assetはsecret-freeを基本とし、Talos machine secrets、Kubernetes PKI private key、Bootstrap Data、kubeconfig、BMC credentialをlog、Event、Statusへ出力しません。

実装後のFresh machine、Storage、Recovery、Safetyの確認項目は[検証方針](../development/verification.md)を参照してください。
