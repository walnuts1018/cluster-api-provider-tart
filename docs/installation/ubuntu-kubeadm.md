# Ubuntu 24.04 と kubeadm の実機導入

> [!WARNING]
> Tart は開発中です。本番環境には使用せず、隔離した Provisioning L2 と消去してよいディスクだけを
> 使用してください。初期 Provisioning により対象 Host のディスクは消去されます。

この手順では、Provider image、iPXE、Provisioning Agent Artifact、OS Artifact、署名鍵を利用者が
ビルドまたはアップロードする必要はありません。同じ Tart Release が配布する manifest と
`cluster-template-kubeadm-ubuntu.yaml` を使用します。

対象は、Ubuntu 24.04、amd64、UEFI、iPXE、Wake-on-LAN を使う隔離 Provisioning L2 です。Provider は
ProxyDHCP として動作するため、既存 DHCP の IP アドレス配布を置き換えません。

## 事前条件

- Kubernetes 管理クラスタへ `kubectl` で接続できること
- Provider Pod が起動する管理クラスタ node と対象 PC が、同じ隔離 Provisioning L2 に接続されていること
- 対象 PC で UEFI network boot と Wake-on-LAN を有効にしていること
- GitHub Container Registry と GitHub Release から Tart Release を取得できること

初回 boot では、隔離 Provisioning L2 上の HTTP 経路で Agent の公開鍵と Agent API 証明書を取得します。
一般利用 LAN と同じ broadcast domain でこの構成を使ってはいけません。

## Release を選ぶ

利用する Tart Release を 1 つ選びます。以降の Provider manifest とクラスターテンプレートは、必ず同じ
`TART_VERSION` のものを使用してください。

```bash
export TART_VERSION=REPLACE_WITH_TART_RELEASE_VERSION
export TART_RELEASE_URL="https://github.com/walnuts1018/cluster-api-provider-tart/releases/download/${TART_VERSION}"
```

Release に次のファイルがあることを確認します。

- `infrastructure-components.yaml`: Provider image、iPXE、Provisioning Agent Artifact と信頼情報を含む導入 manifest
- `metadata.yaml`: `clusterctl` 用の Provider metadata
- `operator-provider.yaml`: Cluster API Operator 用の `InfrastructureProvider`
- `cluster-template-kubeadm-ubuntu.yaml`: Release 済み OS Artifact の digest を固定した kubeadm テンプレート

## Provider を導入する

Cluster API Operator または `clusterctl` のどちらか一方を選びます。どちらの経路でも、利用者が image や
Artifact を作成・公開する必要はありません。

### Cluster API Operator を使う

Cluster API Operator が未導入なら、先に導入します。

```bash
helm repo add capi-operator https://kubernetes-sigs.github.io/cluster-api-operator
helm repo update
helm install capi-operator capi-operator/cluster-api-operator \
  --namespace capi-operator-system \
  --create-namespace \
  --set cert-manager.enabled=true \
  --wait --timeout 90s
```

管理クラスタへ CAPI core、CABPK、KCP が未導入なら、次を一度だけ適用します。

```yaml
apiVersion: operator.cluster.x-k8s.io/v1alpha2
kind: CoreProvider
metadata:
  name: cluster-api
  namespace: capi-system
---
apiVersion: operator.cluster.x-k8s.io/v1alpha2
kind: BootstrapProvider
metadata:
  name: kubeadm
  namespace: capi-kubeadm-bootstrap-system
---
apiVersion: operator.cluster.x-k8s.io/v1alpha2
kind: ControlPlaneProvider
metadata:
  name: kubeadm
  namespace: capi-kubeadm-control-plane-system
```

Tart Provider を導入します。

```bash
kubectl apply -f "${TART_RELEASE_URL}/operator-provider.yaml"
kubectl wait --for=condition=Ready infrastructureprovider/tart \
  -n cluster-api-provider-tart-system --timeout=10m
kubectl -n cluster-api-provider-tart-system rollout status deployment/cluster-api-provider-tart-controller-manager --timeout=10m
```

### `clusterctl` を使う

`clusterctl` の設定へ Tart Release を登録して初期化します。

```bash
mkdir -p "${XDG_CONFIG_HOME:-$HOME/.config}/cluster-api"
cat > "${XDG_CONFIG_HOME:-$HOME/.config}/cluster-api/clusterctl.yaml" <<EOF
providers:
- name: tart
  type: InfrastructureProvider
  url: ${TART_RELEASE_URL}/infrastructure-components.yaml
EOF

clusterctl init \
  --core cluster-api \
  --bootstrap kubeadm \
  --control-plane kubeadm \
  --infrastructure tart

kubectl -n cluster-api-provider-tart-system rollout status deployment/cluster-api-provider-tart-controller-manager --timeout=10m
```

Agent Plan 秘密鍵を格納する `tart-provisioning-credentials` Secret と、起動 node の IP アドレスを SAN に
含む Agent API 証明書は、Provider Pod の init container が自動生成します。Secret が既に存在する場合は
既存の鍵を再利用するため、利用者が Secret を作成、取得、登録、更新する必要はありません。Secret が
削除された場合は、次回の Pod 起動時に新しい鍵を生成します。既存の Provisioning Agent が保持する公開鍵
と一致しなくなるため、稼働中の環境で Secret を削除しないでください。

旧版から更新する場合、旧版の `provisioning-credential-init` Job が残っていれば、新しい Provider Pod の
起動後に削除できます。新方式ではこの Job を使用しません。

```bash
kubectl -n cluster-api-provider-tart-system delete job provisioning-credential-init --ignore-not-found
```

Provider Pod は任意の管理クラスタ node へ schedule されます。Provisioning L2 へ接続していない node が
ある場合だけ、Provider の Deployment に node selector を設定して接続済み node を選択してください。

## TartHost を登録する

物理 PC ごとに、実機から確認した UUID、boot MAC、disk by-id と serial または WWN を指定します。これらは
物理資産を誤って別の PC へ書き込まないために必須です。

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: TartHost
metadata:
  name: cp-01
  labels:
    tart.walnuts.dev/role: control-plane
spec:
  identifiers:
    systemUUID: REPLACE_WITH_SYSTEM_UUID
    bootMACAddress: "00:11:22:33:44:55"
  architecture: amd64
  firmware: UEFI
  platformProfile: amd64-uefi-ab-ubuntu-24.04-kubeadm/v1
  rootDeviceHints:
    deviceName: /dev/disk/by-id/REPLACE_WITH_STABLE_DISK_ID
    serialNumber: REPLACE_WITH_DISK_SERIAL
    minSizeBytes: 68719476736
  management:
    powerDriver: wol
    bootDriver: ipxe
```

worker 用には、`metadata.name`、MAC、UUID、disk identity を変え、`tart.walnuts.dev/role: worker` を付けた
`TartHost` を追加します。Provider が `Available` と表示した Host だけをクラスタ作成に使います。

```bash
kubectl get tarthost
```

## ワークロードクラスタを作成する

Release 済みのテンプレートを適用し、クラスタ名、control plane endpoint、台数だけを環境に合わせて設定します。
OS Artifact の digest 固定参照は Release 作成時にテンプレートへ設定済みです。

```bash
curl -fsSLO "${TART_RELEASE_URL}/cluster-template-kubeadm-ubuntu.yaml"

export CLUSTER_NAME=ubuntu-kubeadm
export CONTROL_PLANE_ENDPOINT_HOST=192.168.100.100
export CONTROL_PLANE_MACHINE_COUNT=1
export WORKER_MACHINE_COUNT=1
export KUBERNETES_VERSION=v1.36.2

envsubst < cluster-template-kubeadm-ubuntu.yaml | kubectl apply -f -
```

進捗は次で確認します。

```bash
kubectl get cluster,tartcluster,tartmachine,tarthost,tarthostoperation
kubectl -n cluster-api-provider-tart-system logs deployment/cluster-api-provider-tart-controller-manager -c manager
```

Provider 以外の image、iPXE、Artifact、鍵をローカルでビルドしたり、registry へ公開したりする必要はありません。
独自 OS Artifact を使う場合だけは、[開発者向けドキュメント](../development/README.md)を参照して別途作成・署名します。
