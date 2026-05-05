# インストールと最初のクラスタ作成

このガイドでは、すでに用意済みの management cluster に Cluster API Operator と cluster-api-provider-tart を導入し、最初の workload cluster を作成する流れを説明します。対象読者は Cluster API をこれから触る人を想定しています。

## このガイドでできること

- Cluster API Operator を使って Cluster API と Tart Provider をまとめてインストールする
- 物理ホストを `TartHost` として登録する
- kubeadm 用テンプレートから workload cluster のマニフェストを生成する
- 生成したマニフェストを適用し、作成後の状態を確認する

## 前提条件

- Kubernetes v1.35 以降の management cluster がある
- `kubectl` で management cluster に接続できる
- `clusterctl` コマンドを利用できる
- PXE ブート対象の物理マシンに到達できるネットワークがある
- `cluster-api-provider-tart` のコントローラーが利用する UDP `67`, UDP `69`, TCP `8082` を開けられる

管理クラスタの作成方法は自由ですが、このガイドでは「management cluster はすでに存在している」前提で進めます。

## kind で management cluster を作る場合

<details>
<summary>kind で最小の management cluster を作る例</summary>

ローカル検証だけ先に試したい場合は、v1.35 以上の Kubernetes Node Image を選んだうえで、次のように kind クラスタを作成できます。

```bash
cat <<'EOF' > kind-management-cluster.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
EOF

kind create cluster --name capi-tart --config kind-management-cluster.yaml
kubectl cluster-info --context kind-capi-tart
```

</details>

## Step 1. Cluster API Operator をインストールする

まず Cluster API Operator を入れます。Operator を使うと、Cluster API の core/bootstrap/control plane/infrastructure provider をまとめて宣言的に管理できます。

```bash
kubectl apply -f https://github.com/kubernetes-sigs/cluster-api-operator/releases/latest/download/operator-components.yaml
kubectl get pods -n capi-operator-system
```

`capi-operator-system` Namespace の Pod が `Running` になれば次へ進めます。

## Step 2. Provider 一式をインストールする

次に、Cluster API 本体と kubeadm/Tart provider をまとめてインストールします。以下の values はこのガイドで前提にする完全なサンプルです。

`enableHelmHook: false` は重要です。これを付けないと Operator の同期時に Helm hook が再実行され、Namespace ごと削除されることがあります。そのため、このガイドでは明示的に無効化します。

```yaml
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
enableHelmHook: false # これをつけないと、毎回Syncする時にnamespaceごと消える
```

たとえば `capi-operator-values.yaml` という名前で保存して、次を実行します。

```bash
helm upgrade --install capi-operator cluster-api-operator/cluster-api-operator \
  --namespace capi-operator-system \
  --reuse-values \
  -f capi-operator-values.yaml
```

すでに Operator 本体は動いているため、この手順では「どの provider を入れるか」を values で宣言しています。

## Step 3. Provider の状態を確認する

インストール直後は、Operator が複数の provider を順番に展開します。まずは登録状態を確認します。

```bash
kubectl get providers.clusterctl.cluster.x-k8s.io -A
kubectl get pods -n capi-operator-system
kubectl get pods -n cluster-api-system
kubectl get pods -n cluster-api-kubeadm-bootstrap-system
kubectl get pods -n cluster-api-kubeadm-control-plane-system
kubectl get pods -n cluster-api-provider-tart-system
```

`Provider` の `STATUS` が `Installed` または `Ready` になり、各 Namespace の controller Pod が `Running` ならインストール完了です。

## Step 4. TartHost を登録する

`TartHost` は、どの物理マシンを provider が利用できるかを表すインベントリです。まずは最小構成で 1 台登録します。

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: TartHost
metadata:
  name: worker-01
spec:
  macAddr: "52:54:00:12:34:56"
```

適用例:

```bash
kubectl apply -f tarthost.yaml
kubectl get tarthosts
```

この時点では、ホストが「Cluster API から割り当て可能な候補」として登録された状態です。

## Step 5. kubeadm クラスタ用の変数を準備する

workload cluster のマニフェストは、[config/templates/cluster-template-kubeadm.yaml](./config/templates/cluster-template-kubeadm.yaml) を `clusterctl generate cluster` で展開して作ります。`clusterctl generate cluster` を使う理由は、テンプレート内の変数をまとめて置換しつつ、Cluster API が扱いやすい完成済みマニフェストを一度に生成できるためです。

このテンプレートでは、少なくとも次の環境変数が必要です。

```bash
export CLUSTER_NAME=tart-quickstart
export KUBERNETES_VERSION=v1.33.0
export CONTROL_PLANE_ENDPOINT_HOST=192.0.2.10
export UBUNTU_KERNEL_URL=http://198.51.100.20:8082/images/ubuntu/vmlinuz
export UBUNTU_INITRD_URL=http://198.51.100.20:8082/images/ubuntu/initrd
export BOOTSTRAP_METADATA_URL=http://198.51.100.20:8082/metadata
```

必要に応じて、テンプレートのデフォルト値を上書きするために次の変数も追加できます。

- `CONTROL_PLANE_ENDPOINT_PORT`
- `CONTROL_PLANE_MACHINE_COUNT`
- `WORKER_MACHINE_COUNT`
- `POD_CIDR`
- `SERVICE_CIDR`

`UBUNTU_KERNEL_URL` と `UBUNTU_INITRD_URL` は、Tart Controller から到達できる HTTP URL を指定してください。`BOOTSTRAP_METADATA_URL` はテンプレート中で `ds=nocloud-net;s=...` として利用されます。

## Step 6. workload cluster のマニフェストを生成する

環境変数を設定したら、テンプレートから実際に適用するマニフェストを生成します。生成結果を `cluster.yaml` に保存しておくと、内容確認や再適用がしやすくなります。

```bash
clusterctl generate cluster "${CLUSTER_NAME}" \
  --from ./config/templates/cluster-template-kubeadm.yaml \
  > cluster.yaml
```

生成後は、`cluster.yaml` に `Cluster`、`KubeadmControlPlane`、`MachineDeployment`、`TartMachineTemplate` が含まれていることを確認してください。

## Step 7. workload cluster を作成する

生成した `cluster.yaml` を management cluster に適用します。ここで初めて Cluster API が `TartMachine` や `Machine` を作成し、登録済みの `TartHost` を使ったプロビジョニングが始まります。

```bash
kubectl apply -f cluster.yaml
```

適用直後はすぐに `Ready` にならなくても問題ありません。Cluster API が順番にリソースを作成し、物理ホストの起動や bootstrap を進めます。

## Step 8. 作成後の状態を確認する

作成後は、Cluster API と Tart Provider の両方のリソースを見ます。

```bash
kubectl get clusters,machines,kubeadmcontrolplanes,tartmachines,tarthosts -A
kubectl describe cluster "${CLUSTER_NAME}"
```

確認の見方:

- `Cluster` に `ControlPlaneReady` や `InfrastructureReady` が付くか
- `Machine` が `Provisioning` から `Running` 相当に進むか
- `TartHost` が対象クラスタへ割り当てられているか
- `TartMachine` が bootstrap 用の情報を取得しているか

## トラブルシューティング

初心者向けに、まず見るべき確認コマンドをまとめます。

```bash
kubectl get providers.clusterctl.cluster.x-k8s.io -A
kubectl get clusters,machines,kubeadmcontrolplanes,tartmachines,tarthosts -A
kubectl logs -n capi-operator-system deploy/capi-operator-controller-manager
kubectl logs -n cluster-api-provider-tart-system -l control-plane=controller-manager --tail=100
```

よくある確認ポイント:

- provider が `Ready` にならない場合: Operator ログを確認し、values の `fetchConfig.url` や version を見直す
- `TartHost` が使われない場合: `macAddr` が対象ホストの NIC と一致しているか確認する
- `TartMachine` が進まない場合: PXE 対象ホストから UDP `67`/`69` と TCP `8082` へ到達できるか確認する
- kubeadm bootstrap が失敗する場合: `UBUNTU_KERNEL_URL`、`UBUNTU_INITRD_URL`、`BOOTSTRAP_METADATA_URL` がテンプレートと一致しているか確認する

## クリーンアップ

検証をやり直す場合は、まず workload cluster のリソースを削除し、その後に `TartHost` を片付けます。

```bash
kubectl delete -f cluster.yaml
kubectl delete tarthost worker-01
```

provider 一式も削除したい場合は、Operator に渡した values から `infrastructure.tart` などを外して再同期するか、検証用 management cluster 自体を削除してください。kind を使っているなら、最後は次でまとめて消せます。

```bash
kind delete cluster --name capi-tart
```
