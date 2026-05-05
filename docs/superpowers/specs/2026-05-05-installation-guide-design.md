# installation.md 再構成設計

## 目的

`installation.md` を、`cluster-api-provider-tart` を初めて触る利用者でも順番に追える導入ガイドへ全面的に作り直す。

このガイドでは、既存の management cluster に対して Cluster API Operator を使って Provider 群を導入し、`kubeadm` を使う workload cluster を実際に作成して確認するところまでを扱う。

## 対象読者

- Cluster API にまだ慣れていない利用者
- `cluster-api-provider-tart` の初回導入を行う利用者
- 既存の management cluster は持っているが、Provider の導入方法や最初のクラスタ作成手順を知りたい利用者

## ゴール

利用者が `installation.md` を読み終えた時点で、以下を再現できる状態にする。

- Cluster API Operator を management cluster に導入できる
- 指定された values を使って `cluster-api`, `kubeadm`, `tart` Provider を導入できる
- `TartHost` を登録できる
- `cluster-template-kubeadm.yaml` を元に workload cluster 用の YAML を生成して適用できる
- 作成した Cluster / Machine / TartHost の状態確認ができる

## スコープ

### 含める内容

- `cluster-api-operator` を使う唯一の導入方法
- Operator 用 `values.yaml` の完全なサンプル
- 各ステップで何をしているのかの説明
- そのまま試せるコマンド例
- `TartHost` のサンプルマニフェスト
- `clusterctl generate cluster` を使った kubeadm クラスタ作成例
- 主要な確認コマンド
- 初心者が詰まりやすいポイントに絞ったトラブルシュート
- `<details>` に折りたたんだ `kind` 管理クラスタ作成例

### 含めない内容

- Helm を使った導入手順
- Talos 用クラスタ作成手順
- 本番向けの高可用性設計や詳細なネットワーク設計
- Cluster API 自体の包括的な入門

## 構成案

`installation.md` は次の順序で再構成する。

1. このガイドでできること
2. 前提条件
3. `kind` で management cluster を用意する補足手順 (`<details>`)
4. Cluster API Operator のインストール
5. 指定された values を使った Provider 一式の導入
6. Provider の Ready 確認
7. `TartHost` の登録
8. kubeadm クラスタ用テンプレートの使い方
9. workload cluster の作成
10. 作成後の確認
11. トラブルシュート
12. クリーンアップ

## 重要な記述方針

- 管理クラスタは「すでに存在している」前提で書く
- 各コマンドの前後に、なぜその操作が必要かを短く説明する
- 変数を使う箇所では、各変数の意味と具体例をセットで示す
- 初心者向けに、`kubectl` の確認対象リソースを明示する
- `enableHelmHook: false` の理由はコメント付きでそのまま掲載する
- 説明は日本語で書き、利用者が直接適用する YAML やコマンド例は実行可能性を優先する

## 掲載する主要サンプル

### Cluster API Operator 用 values

ユーザー指定の内容をそのまま採用する。

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

### TartHost 例

最低限の登録例を掲載する。MAC アドレスや BMC を使わない前提に合わせ、在庫管理の最小サンプルとして示す。

### workload cluster 作成例

- `clusterctl generate cluster` を使う
- テンプレートは `config/templates/cluster-template-kubeadm.yaml` を使う
- `CLUSTER_NAME`, `KUBERNETES_VERSION`, `CONTROL_PLANE_ENDPOINT_HOST`, `UBUNTU_KERNEL_URL`, `UBUNTU_INITRD_URL`, `BOOTSTRAP_METADATA_URL` など、テンプレート展開に必要な環境変数を説明する
- 生成結果をファイルに保存して `kubectl apply -f` する流れを示す

## エラーハンドリングとトラブルシュート

以下の失敗を想定した短い対処を載せる。

- Provider が Ready にならない
- `TartHost` が割り当て対象にならない
- `clusterctl generate cluster` で環境変数不足になる
- PXE 関連ポートが利用できず controller が起動しない

## 変更対象

- 主対象: `installation.md`
- 必要に応じて、`README.md` からの導線文言だけ最小限見直す

## 実装時の注意

- 既存の Helm セクションは完全に削除する
- 古いバージョン番号や `InfrastructureProvider` 単体適用例は残さない
- 現在の `config/templates/cluster-template-kubeadm.yaml` と整合する説明だけを書く
- 初心者向けでも冗長になりすぎないよう、詳細説明は補足セクションや `<details>` に寄せる
