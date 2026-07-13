# インストールと最初のクラスタ作成

このガイドでは、すでに用意済みの management cluster に Cluster API Operator と cluster-api-provider-tart を導入し、workload cluster を作成する流れを説明します。

## このガイドでできること

- Cluster API Operator を使って Cluster API と Tart Provider をインストールする
- Cluster APIを用いて、物理マシンにUbuntu 26.04をインストールして、Kubeadmクラスタを作成する

## 用語の定義

- workload cluster: Cluster API と Tart Provider を使って物理ホスト上に作成するKubernetes クラスタ。
- management cluster: Cluster API をインストールするクラスタ。workload cluster を管理する。

## 前提条件

- Kubernetes v1.35 以降の management cluster がある
- `kubectl` で management cluster に接続できる
- Gateway API の `HTTPRoute` を使える環境がある
- Wake on LANが有効化された物理ホストが用意されていて、MACアドレスなどの情報がわかっている

## Step 1. Cluster API Operator と Provider 一式をインストールする

まず Cluster API Operator を入れます。Cluster API の core/bootstrap/control plane/infrastructure provider を宣言的に管理できるようになります。

Cluster API OperatorをHelmでインストールする場合は、Providerも一緒にインストールすることができます。
Helm以外を用いる場合は、Operatorを先にインストールしてから、各種 Provider リソースを manifest で管理クラスタに適用してください。

```values.yaml
core:
  cluster-api: {}
bootstrap:
  kubeadm: {}
controlPlane:
  kubeadm: {}
infrastructure:
  tart:
    version: v0.0.2
    fetchConfig:
      url: https://github.com/walnuts1018/cluster-api-provider-tart/releases/v0.0.2/infrastructure-components.yaml
resources:
  manager: {}

# ArgoCDでは以下の設定を入れないと毎回Syncする時にnamespaceごと消えてしまう
# enableHelmHook: false
```

```bash
helm repo add cluster-api-operator https://kubernetes-sigs.github.io/cluster-api-operator
helm install capi-operator cluster-api-operator/cluster-api-operator \
  --namespace capi-operator-system \
  -f values.yaml
```

## Step 2. TartHost を登録する

`TartHost` は、物理マシンと対応するリソースです。Cluster API が workload cluster を作成する際に、登録された `TartHost` を使ってプロビジョニングを行います。

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: TartHost
metadata:
  name: worker-01
spec:
  identifiers:
    bootMACAddress: "00:00:5e:00:53:01"
  architecture: amd64
  firmware: UEFI
  platformProfile: amd64-uefi-ab/v1
  rootDeviceHints:
    deviceName: /dev/disk/by-id/wwn-0x5000000000000001
    minSizeBytes: 274877906944
  management:
    powerDriver: wol
    bootDriver: ipxe
```

```bash
kubectl apply -f tart-host.yaml
```

## Step 3. kubeadm クラスタ用の sample manifest を確認する

workload cluster の雛形は [config/samples/cluster-kubeadm-ubuntu.yaml](./config/samples/cluster-kubeadm-ubuntu.yaml) にあります。
この sample は v1beta1 の TartCluster/TartMachineTemplate を使い、digest固定のOCI OS Artifactを参照します。

## Step 5. workload cluster を作成する

確認した `cluster.yaml` を management cluster に適用します。
ここで初めて Tart Managerが物理マシンを起動・プロビジョニングし、kubeadm bootstrap を開始します。

```bash
kubectl apply -f cluster.yaml
```
