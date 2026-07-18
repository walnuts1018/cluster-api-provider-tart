# Cluster API OperatorによるTart Provider導入

この手順では、利用者はProvider image、iPXE、OS Artifact、Provisioning Agent Artifact、署名公開鍵、TLS証明書、Agent Plan鍵を作成または登録しません。同じReleaseの`operator-provider.yaml`を適用すると、Providerが必要な公開鍵を含む構成で導入されます。

対象はUbuntu 24.04、amd64、UEFI、iPXE、Wake-on-LANを使う隔離Provisioning L2です。ProviderはProxyDHCPとして動作し、既存DHCPのIPアドレス配布を置き換えません。

## 事前条件

- Kubernetes管理クラスタへ`kubectl`で接続できること
- Provider Podが起動する管理クラスタnodeと対象PCが、同じ隔離Provisioning L2に接続されていること
- 対象PCでUEFI network bootとWake-on-LANを有効にしていること
- GitHub Container RegistryからRelease imageをpullできること

初回bootでは隔離Provisioning L2上のHTTP経路で公開鍵とAgent API証明書を取得します。一般利用LANと同じbroadcast domainでこの構成を使うことは禁止です。

## 1. Cluster API Operatorを導入する

```bash
helm repo add capi-operator https://kubernetes-sigs.github.io/cluster-api-operator
helm repo update
helm install capi-operator capi-operator/cluster-api-operator \
  --namespace capi-operator-system \
  --create-namespace \
  --set cert-manager.enabled=true \
  --wait --timeout 90s
```

管理クラスタへCAPI core、CABPK、KCPが未導入なら、次を一度だけ適用します。

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

## 2. Tart Providerを導入する

GitHub Releaseから同じtagの`operator-provider.yaml`を取得して適用します。

```bash
kubectl apply -f operator-provider.yaml
kubectl wait --for=condition=Ready infrastructureprovider/tart \
  -n cluster-api-provider-tart-system --timeout=10m
kubectl -n cluster-api-provider-tart-system get job provisioning-credential-init
```

`provisioning-credential-init` JobはAgent Plan秘密鍵を管理クラスタ内に一度だけ生成します。Provider Podのinit containerは、起動nodeのIPアドレスをSANに含む短期間のAgent API証明書を自動生成します。これらの値を利用者が取得、登録、更新する必要はありません。

Provider Podは任意の管理クラスタnodeへscheduleされます。複数nodeのうちProvisioning L2へ接続していないnodeがある場合だけ、`InfrastructureProvider.spec.deployment.nodeSelector`で接続済みnodeを選択してください。

## 3. TartHostを登録する

物理PCごとに、実機から確認したUUID、boot MAC、disk by-idとserial/WWNを指定します。これらは物理資産を誤って別PCへ書き込まないために必須です。

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

workerには`TartHost`を追加し、`tart.walnuts.dev/role: worker`を設定します。Providerが`Available`と表示したHostだけをクラスタ作成に使います。

```bash
kubectl get tarthost
```

## 4. ワークロードクラスタを作成する

Releaseの`cluster-template-kubeadm-ubuntu.yaml`を使い、クラスタ名、control plane endpoint、台数だけを環境に合わせて設定します。OS Artifactのdigest固定参照はRelease作成時にテンプレートへ設定済みです。

```bash
export CLUSTER_NAME=ubuntu-kubeadm
export CONTROL_PLANE_ENDPOINT_HOST=192.168.100.100
export CONTROL_PLANE_MACHINE_COUNT=1
export WORKER_MACHINE_COUNT=1
export KUBERNETES_VERSION=v1.36.2

envsubst < cluster-template-kubeadm-ubuntu.yaml | kubectl apply -f -
```

Provider以外のArtifactや鍵をローカルでビルドする必要はありません。

進捗は次で確認します。

```bash
kubectl get cluster,tartcluster,tartmachine,tarthost,tarthostoperation
kubectl -n cluster-api-provider-tart-system logs deployment/cluster-api-provider-tart-controller-manager -c manager
```

## Release管理者向けGitHub Actions Secret

利用者ではなくRelease管理者だけが、次のRepository Secretを登録します。

| Secret名 | 用途 |
| --- | --- |
| `OS_ARTIFACT_SIGNING_KEY` | OS Artifact署名用Ed25519秘密鍵 |
| `OS_ARTIFACT_PUBLIC_KEY_PEM` | OS Artifact署名公開鍵。Agent ArtifactとProvider manifestへ同梱する |
| `AGENT_ARTIFACT_SIGNING_KEY` | Provisioning Agent Artifact署名用Ed25519秘密鍵 |

`GITHUB_TOKEN`はActionsが自動提供します。`AGENT_PLAN_PUBLIC_KEY_PEM`は不要になりました。Agent Plan鍵は各管理クラスタへ自動生成され、秘密鍵がGitHub ReleaseやGHCRへ公開されることはありません。

```bash
openssl genpkey -algorithm Ed25519 -out os-artifact-signing-key.pem
openssl pkey -in os-artifact-signing-key.pem -pubout -out os-artifact-public.pem
openssl genpkey -algorithm Ed25519 -out agent-artifact-signing-key.pem

gh secret set OS_ARTIFACT_SIGNING_KEY < os-artifact-signing-key.pem
gh secret set OS_ARTIFACT_PUBLIC_KEY_PEM < os-artifact-public.pem
gh secret set AGENT_ARTIFACT_SIGNING_KEY < agent-artifact-signing-key.pem
```
