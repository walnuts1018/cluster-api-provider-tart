# インストール方法

cluster-api-provider-tart のインストールには、以下の2つの方法があります。

- [Helm を利用したインストール](#helm を利用したインストール)
- [cluster-api-operator を利用したインストール](#cluster-api-operator を利用したインストール)

どちらの方法でも同じコントローラーが導入されますが、管理のしやすさや既存のインフラとの整合性に応じて選択してください。

## 前提条件

以下の環境が整っていることを確認してください。

- Kubernetes クラスター (v1.28 以降)
- `kubectl` コマンドがクラスターに接続できる状態
- Helm v3 以降 (Helm インストール場合のみ)
- cluster-api-operator (cluster-api-operator インストール場合のみ)

## クラスター API の事前インストール

cluster-api-provider-tart は Infrastructure Provider であり、Cluster API コアコンポーネントとは別にインストールする必要があります。

以下のコンポーネントが management cluster にインストールされていることを確認してください。

- **Core Provider**: `cluster-api`
- **Bootstrap Provider**: `kubeadm` または `talos`
- **Control Plane Provider**: `kubeadm` または `talos`

インストール済みの確認方法:

```bash
kubectl get clusters.cluster.x-k8s.io -A
```

空の結果が返ってくるのは正常です（クラスターがまだ作成されていないため）。代わりに、以下のようしてプロバイダーが登録されているかを確認できます。

```bash
kubectl get infrastructureproviders -A
kubectl get bootstrapproviders -A
kubectl get controlplaneproviders -A
```

## Helm を利用したインストール

Helm を利用して cluster-api-provider-tart をインストールします。

### 1. Helm リポジトリの追加

```bash
helm repo add cluster-api-provider-tart https://walnuts1018.github.io/cluster-api-provider-tart/
helm repo update
```

### 2. Helm Chart のインストール

```bash
helm install cluster-api-provider-tart cluster-api-provider-tart/cluster-api-provider-tart \
  --namespace cluster-api-provider-tart-system \
  --create-namespace \
  --set controllerManager.manager.image.tag=v0.1.0
```

### 3. インストールの確認

```bash
kubectl get pods -n cluster-api-provider-tart-system
```

以下のような出力が得られるはずです。

```
NAME                                                              READY   STATUS    RESTARTS   AGE
cluster-api-provider-tart-controller-manager-xxxxx-yyyy   1/1     Running   0          30s
```

### 4. 設定のカスタマイズ

`values.yaml` の値を上書きすることで、インストールをカスタマイズできます。

```bash
helm install cluster-api-provider-tart cluster-api-provider-tart/cluster-api-provider-tart \
  --namespace cluster-api-provider-tart-system \
  --create-namespace \
  --set controllerManager.manager.image.tag=v0.1.0 \
  --set controllerManager.replicas=2 \
  --set controllerManager.nodeSelector.kubernetes.io/os=linux
```

利用可能な設定値の詳細は、[values.yaml](./charts/cluster-api-provider-tart/values.yaml) を参照してください。

### 5. アンインストール

```bash
helm uninstall cluster-api-provider-tart -n cluster-api-provider-tart-system
```

CRD を削除する場合は:

```bash
kubectl delete crd tartclusters.cluster.x-k8s.io tartclustertemplates.cluster.x-k8s.io tartmachines.cluster.x-k8s.io tartmachinetemplates.cluster.x-k8s.io tarthosts.cluster.x-k8s.io
```

## cluster-api-operator を利用したインストール

cluster-api-operator を利用して、宣言的に cluster-api-provider-tart をインストールします。

### cluster-api-operator のインストール

まだ cluster-api-operator がインストールされていない場合は、以下のようにインストールしてください。

```bash
kubectl apply -f https://github.com/kubernetes-sigs/cluster-api-operator/releases/latest/download/operator-components.yaml
```

operator が起動するまで待機します。

```bash
kubectl get pods -n cluster-api-operator-system
```

### 1. InfrastructureProvider リソースの作成

`InfrastructureProvider` リソースを作成することで、cluster-api-provider-tart が自動的にインストールされます。

cluster-api-operator は、リリースから `infrastructure-components.yaml` と `metadata.yaml` を自動的に取得します。

```yaml
apiVersion: operator.cluster.x-k8s.io/v1alpha2
kind: InfrastructureProvider
metadata:
  name: tart
  namespace: cluster-api-provider-tart-system
spec:
  version: v0.1.0
  fetchConfig:
    url: https://github.com/walnuts1018/cluster-api-provider-tart/releases
```

これを適用します。

```bash
kubectl apply -f tart-provider.yaml
```

### 2. インストールの確認

```bash
kubectl get infrastructureproviders tart -o yaml
```

`STATUS` が `Ready` になればインストール成功です。

```yaml
status:
  conditions:
  - lastTransitionTime: "2025-01-01T00:00:00Z"
    message: Provider tart successfully installed
    status: "True"
    type: Ready
  observedGeneration: 1
  version: v0.1.0
```

また、コントローラーの Pod も確認できます。

```bash
kubectl get pods -n cluster-api-provider-tart-system
```

### 3. 設定のカスタマイズ

インストール後に設定を変更する場合は、`InfrastructureProvider` リソースを編集します。

```bash
kubectl edit infrastructureprovider tart -n cluster-api-provider-tart-system
```

### 4. アップグレード

バージョンを変更して適用することで、アップグレードできます。

```yaml
apiVersion: operator.cluster.x-k8s.io/v1alpha2
kind: InfrastructureProvider
metadata:
  name: tart
  namespace: cluster-api-provider-tart-system
spec:
  version: v0.2.0
  fetchConfig:
    url: https://github.com/walnuts1018/cluster-api-provider-tart/releases
```

### 5. アンインストール

`InfrastructureProvider` リソースを削除します。

```bash
kubectl delete infrastructureprovider tart -n cluster-api-provider-tart-system
```

CRD を削除する場合は:

```bash
kubectl delete crd tartclusters.cluster.x-k8s.io tartclustertemplates.cluster.x-k8s.io tartmachines.cluster.x-k8s.io tartmachinetemplates.cluster.x-k8s.io tarthosts.cluster.x-k8s.io
```

## 両方のインストール方法の比較

| 項目 | Helm | cluster-api-operator |
|------|------|---------------------|
| 管理方式 | Imperative (命令型) | Declarative (宣言型) |
| バージョン管理 | Chart バージョン | Provider バージョン |
| アップグレード | `helm upgrade` | `InfrastructureProvider` を編集 |
| ロールバック | `helm rollback` | `InfrastructureProvider` を編集 |
| 依存関係の管理 | 手動 | 自動 (CoreProvider を自動検出) |
| 適したユースケース | 単一クラスター、柔軟な設定 | 複数のクラスター、一貫した管理 |

## ネットワーク要件

cluster-api-provider-tart は、PXE ブートのために以下のポートを使用します。インストール時には、これらのポートが開放されていることを確認してください。

| ポート | プロトコル | 用途 |
|--------|-----------|------|
| 67 | UDP | ProxyDHCP (iPXE ブートローダの配信) |
| 69 | UDP | TFTP (iPXE バイナリの配信) |
| 8082 | TCP | iPXE スクリプト・メタデータ配信 |

コントローラーは `hostNetwork: true` で実行されるため、ホストのネットワークインターフェースを通じてこれらのポートが公開されます。

## トラブルシューティング

### コントローラーが起動しない

```bash
kubectl logs -n cluster-api-provider-tart-system -l control-plane=controller-manager --tail=100
```

### ポートが使用されている

コントローラーがポート 67, 69, 8082 を使用できない場合、ログにエラーが出力されます。既存の DHCP サーバーや TFTP サーバーと競合していないか確認してください。

### CRD が見つからない

```bash
kubectl get crd | grep tart
```

以下の CRD が存在することを確認してください。

- `tartclusters.cluster.x-k8s.io`
- `tartclustertemplates.cluster.x-k8s.io`
- `tartmachines.cluster.x-k8s.io`
- `tartmachinetemplates.cluster.x-k8s.io`
- `tarthosts.cluster.x-k8s.io`
