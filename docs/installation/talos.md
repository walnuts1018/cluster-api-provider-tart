# Talos Linux導入

TartはTalos Linux専用のCluster API Providerとして再実装中です。現時点では、実用可能なFresh machine、single node、HA control plane、workerのE2Eが成立するまで、利用者向けの導入手順とrelease manifestを提供しません。

## 導入モデル

管理クラスタへCAPI core、TartのInfrastructure Provider、Bootstrap Provider、Control Plane Providerを導入し、`TartHost`へHostのidentityとpower/boot capabilityを登録します。ユーザーはinstall前にLinux device pathやdisk UUIDを登録する必要はありません。in-place updateを使う場合はCAPIの`RuntimeSDK`と`InPlaceUpdates`を有効にし、TartのHTTPS endpointを`ExtensionConfig`へ登録します。

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

未構成Talosのmaintenance APIはTLSで暗号化されますが認証済みではありません。machine configurationを送る前に`TartHost`、boot attempt、MAC/DHCP、endpoint、system UUID/inventoryを結び付け、曖昧なら停止します。installation後はauthenticated Talos APIへ移行します。Machine削除時はshutdownと停止確認後にclaimを解除し、Hostをdata保持の`Retained`として残します。

## 安全性

通常のTalos/Kubernetes updateはin-placeで実行し、CAPI Machine、`TartMachine`、`TartHost`、diskを維持します。Machine削除でもHostの物理dataは保持し、cleaning、reprovisioning、disk wipeは明示的な別操作とします。

初期boot assetはsecret-freeを基本とし、Talos machine secrets、Kubernetes PKI private key、Bootstrap Data、kubeconfig、BMC credentialをlog、Event、Statusへ出力しません。

実装後のFresh machine、Storage、Recovery、Safetyの確認項目は[検証方針](../development/verification.md)を参照してください。
