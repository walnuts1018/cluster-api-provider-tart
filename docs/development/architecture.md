# アーキテクチャ

Tart は、物理 Host を Cluster API の Infrastructure Provider として扱う Go プロジェクトである。
controller-manager は Kubernetes Reconciler と Provisioning 用ネットワークサービスを同一 process で実行し、
対象 Host は Provisioning Agent を通じて操作する。

## コンポーネント

```text
CAPI / Bootstrap Provider
          |
          v
controller-manager
  |- Kubernetes Reconciler
  |- ProxyDHCP / TFTP / HTTPS
  |- WoL / Redfish Driver
          |
          v
Provisioning Agent / Node Lifecycle Service
          |
          v
physical host, disk, and boot firmware
```

- `TartHost` は物理 Host のインベントリを表す長寿命 Resource である。
- `TartMachine` は CAPI Machine に対応する Infrastructure Resource である。
- `TartHostOperation` は Provisioning、更新、cleaning などの長時間処理と再開位置を保持する。
- Bootstrap Provider は Bootstrap Data の生成を所有する。Tart はその内容を解釈せず、安全に配送する。
- Provisioning Agent は署名済み Plan に従って disk と boot を操作する。Kubernetes API を直接更新しない。

## 重要な設計規則

- Controller は Kubernetes I/O、Condition、Eventの起動だけを担当し、状態遷移の判断を直接持たない。
- OS Artifact は `oci://` 形式の OCI image reference を使用する。タグを指定した場合も、Plan に記録した Artifact Manifest digest と署名で取得内容を固定する。
- Secret、Bootstrap Data、Session Token、署名鍵を Resource Status、ログ、テスト出力へ出さない。
- 長時間処理の再開に必要な状態は `TartHostOperation` へ保存し、process 内メモリを正本にしない。

CRD、Webhook、Controller の変更には Kubebuilder または controller-gen を使用する。生成手順は
[開発ガイド](development.md)を参照する。
