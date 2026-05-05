# インストールと最初のクラスタ作成

このガイドでは、すでに用意済みの management cluster に Cluster API Operator と cluster-api-provider-tart を導入し、workload cluster を作成する流れを説明します。
対象読者は Cluster API をこれから触る人を想定しています。

## このガイドでできること

- Cluster API Operator を使って Cluster API と Tart Provider をインストールする
- Cluster APIを用いて、物理マシンにKubeadm クラスタを作成する

## 用語の定義

- workload cluster: Cluster API と Tart Provider を使って物理ホスト上に作成するKubernetes クラスタ
- management cluster: Cluster API をインストールして、workload cluster を管理する Kubernetes クラスタ

## 前提条件

- Kubernetes v1.35 以降の management cluster がある
- `kubectl` で management cluster に接続できる
- Gateway API の `HTTPRoute` を使える環境がある

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
  image: kindest/node:v1.35.0@sha256:452d707d4862f52530247495d180205e029056831160e22870e37e3f6c1ac31f
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

次に、Cluster API 本体と kubeadm/Tart provider をまとめてインストールします。

通常の手動運用では `enableHelmHook: false` は不要です。Argo CD で同期する場合だけ追加してください。

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
# enableHelmHook: false # これをつけないと、毎回Syncする時にnamespaceごと消える
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

`TartHost` は、どの物理マシンを provider が利用できるかを表すインベントリです。まずは 1 台登録します。

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: TartHost
metadata:
  name: worker-01
spec:
  macAddress: "52:54:00:12:34:56"
  bootMacAddress: "52:54:00:12:34:57"
```

適用例:

```bash
kubectl apply -f tarthost.yaml
kubectl get tarthosts
```

`bootMacAddress` は PXE boot に使う NIC が `macAddress` と異なる場合だけ必要です。同じ NIC を使う場合は省略できます。

## Step 5. HTTPRoute を追加する

bootstrap metadata と iPXE script は `HTTPRoute` で公開します。sample は [config/samples/bootstrap-httproute.yaml](./config/samples/bootstrap-httproute.yaml) にあります。

まずは sample をコピーして、利用中の Gateway に合わせて `parentRefs` と `hostnames` を調整してください。

```bash
cp ./config/samples/bootstrap-httproute.yaml bootstrap-httproute.yaml
```

この sample は `controller-manager-ipxe:8082` に `/ipxe` と `/metadata` を転送します。`hostnames` を変更した場合は、後続の `cluster.yaml` にある `ds=nocloud-net;s=...` も同じホスト名へ合わせてください。

適用例:

```bash
kubectl apply -f bootstrap-httproute.yaml
```

## Step 6. kubeadm クラスタ用の sample manifest をコピーする

workload cluster の雛形は [config/samples/cluster-kubeadm.yaml](./config/samples/cluster-kubeadm.yaml) にあります。このファイルには、`Cluster`、`TartCluster`、`KubeadmControlPlane`、`TartMachineTemplate`、`MachineDeployment`、`KubeadmConfigTemplate` がすでに一式そろっています。

まずはこの sample をコピーして、自分の作業用ファイルを作ってください。

```bash
cp ./config/samples/cluster-kubeadm.yaml cluster.yaml
```

## Step 7. sample manifest を自分の環境向けに書き換える

`cluster.yaml` を開いて、最低限次の箇所を自分の環境に合わせて変更します。

- `metadata.name` と `cluster.x-k8s.io/cluster-name`
  - この sample では `tart-kubeadm-sample` になっています。自分のクラスタ名に合わせて、ファイル内の同じ名前をまとめて置き換えてください。
- `spec.controlPlaneEndpoint.host`
  - workload cluster の Kubernetes API に到達するための IP または DNS 名です。
- `spec.version`
  - Control Plane と Worker の Kubernetes バージョンです。
- `image` と `initrd`
  - 起動に使う kernel/initrd の URL です。sample では Ubuntu 26.04 の公開イメージを指定しています。別の OS イメージを使う場合だけ書き換えてください。
- `ds=nocloud-net;s=...`
  - bootstrap metadata を配信する URL です。この sample では `http://bootstrap.sample.walnuts.dev/metadata` を使っています。`bootstrap-httproute.yaml` のホスト名を変えた場合は、ここも同じ値へ変更してください。

sample の初期値は、以下のような読み替えを想定しています。

- `192.0.2.10`:
  管理用ロードバランサや仮想 IP など、Control Plane Endpoint に使う実アドレスへ変更します。
- `bootstrap.sample.walnuts.dev`:
  `bootstrap-httproute.yaml` と `ds=nocloud-net;s=...` で使うホスト名です。自分の環境に合わせて統一して変更します。

必要に応じて、次の値も調整してください。

- `replicas`: Control Plane と Worker の台数
- `clusterNetwork.pods.cidrBlocks` と `clusterNetwork.services.cidrBlocks`: 使いたい Pod/Service CIDR

`config/samples/cluster-kubeadm.yaml` は [config/templates/cluster-template-kubeadm.yaml](./config/templates/cluster-template-kubeadm.yaml) と同じ構成に合わせてあるため、テンプレートを読む前にまずこの sample を編集すれば十分です。

## Step 8. workload cluster を作成する

書き換えた `cluster.yaml` を management cluster に適用します。ここで初めて Cluster API が `TartMachine` や `Machine` を作成し、登録済みの `TartHost` を使ったプロビジョニングが始まります。

```bash
kubectl apply -f cluster.yaml
```

適用直後はすぐに `Ready` にならなくても問題ありません。Cluster API が順番にリソースを作成し、物理ホストの起動や bootstrap を進めます。

## Step 9. 作成後の状態を確認する

作成後は、Cluster API と Tart Provider の両方のリソースを見ます。

```bash
kubectl get clusters,machines,kubeadmcontrolplanes,tartmachines,tarthosts -A
kubectl describe cluster tart-kubeadm-sample
kubectl get httproutes -A
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
kubectl get httproutes -A
kubectl logs -n capi-operator-system deploy/capi-operator-controller-manager
kubectl logs -n cluster-api-provider-tart-system -l control-plane=controller-manager --tail=100
```

よくある確認ポイント:

- provider が `Ready` にならない場合: Operator ログを確認し、values の `fetchConfig.url` や version を見直す
- `TartHost` が使われない場合: `macAddress` と、必要なら `bootMacAddress` の値を見直す
- `HTTPRoute` が想定どおりに使えない場合: `parentRefs`、`hostnames`、`/ipxe` と `/metadata` の path match を確認する
- kubeadm bootstrap が失敗する場合: `cluster.yaml` の `image`、`initrd`、`ds=nocloud-net;s=...` の値を見直す

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
