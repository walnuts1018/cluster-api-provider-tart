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

まっさらなhostがPXE bootでTalos maintenance modeまで自動的に到達できるようにするため、Infrastructure Provider(`config/default`)には`netboot-server`がcontroller-managerとは別Deploymentとして同梱されており、`clusterctl init --infrastructure tart`や`InfrastructureProvider`(cluster-api-operator)でインストールしただけで一緒にデプロイされます。PXEクライアントのMACアドレスから対応する`TartHost`/`TartMachine`を解決し、そのHost向けのdesired Talos version/schematicIDでTalos Image FactoryのPXE配信endpointへchainします。対応するHostがまだ登録されていないMAC(初回enrollment boot)は、`TART_NETBOOT_DISCOVERY_TALOS_VERSION`/`TART_NETBOOT_DISCOVERY_SCHEMATIC_ID`環境変数で設定したdiscovery用Talos version/schematicIDへfallbackします(未設定の場合、初回enrollment bootのhostはmaintenance modeへ到達できません)。複数NICやNAT環境では`TART_NETBOOT_ADVERTISE_HTTP_BASE_URL`でadvertiseするHTTP base URLを明示的に上書きしてください。

netboot-serverはDHCP(67)/ProxyDHCP(4011)/TFTP(69)のwell-knownポートと、broadcastパケットを直接受け取るための`hostNetwork: true`を必要とします(Deploymentへ設定済み)。

### リリースmanifestの取得

`clusterctl init`や`InfrastructureProvider`が参照するGitHub Releaseには、`infrastructure-components.yaml`(controller-managerとnetboot-serverの両方を含む)、`metadata.yaml`、`infrastructure-provider.yaml`(cluster-api-operator用のサンプルCR)が添付されます。手元で同じ内容を生成する場合は`mise run release-manifests`(`CONTROLLER_IMAGE`、`NETBOOT_SERVER_IMAGE`環境変数が必要)を使ってください。

未構成Talosのmaintenance APIはTLSで暗号化されますが認証済みではありません。machine configurationを送る前に`TartHost`、boot attempt、MAC/DHCP、endpoint、system UUID/inventoryを結び付け、曖昧なら停止します。installation後はauthenticated Talos APIへ移行します。Machine削除時はshutdownと停止確認後にclaimを解除し、Hostをdata保持の`Retained`として残します。

## 安全性

通常のTalos/Kubernetes updateはin-placeで実行し、CAPI Machine、`TartMachine`、`TartHost`、diskを維持します。Machine削除でもHostの物理dataは保持し、cleaning、reprovisioning、disk wipeは明示的な別操作とします。

初期boot assetはsecret-freeを基本とし、Talos machine secrets、Kubernetes PKI private key、Bootstrap Data、kubeconfig、BMC credentialをlog、Event、Statusへ出力しません。

達成すべき要件・ユースケースは[開発者向けドキュメント](../development/README.md)を参照してください。
