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

## ソースコードの責務

| ディレクトリ | 責務 |
|---|---|
| `api/` | Kubebuilder が管理する CRD API、validation、defaulting |
| `domain/` | 状態遷移、値オブジェクト、Workflow、期待される失敗 |
| `dto/` | Agent API と Artifact の境界 DTO |
| `infrastructure/` | Kubernetes、HTTP、OCI、Driver、Provisioning Agent などの外部 I/O |
| `cmd/` | controller-manager、Agent、開発用 command の composition root |
| `config/` | Kustomize、CRD、RBAC、導入 manifest |
| `test/` | architecture、fixture、resource preservation、E2E 検証 |

`domain/<context>` は Workflow とそのユビキタス言語を持つ単位である。複数の Context で共有する Domain 型は
`domain/shared/<concept>` に置く。Domain は `api`、`dto`、`infrastructure` の具体実装を import してはならない。
外部 I/O を要求する interface は、それを使う Workflow package に定義し、`infrastructure` が実装する。

## 重要な設計規則

- Controller は Kubernetes I/O、Condition、Event、Workflow の起動だけを担当し、状態遷移の判断を直接持たない。
- Workflow は `Command` を受け、`Result[Event, Failure]` を返す。期待される失敗に標準 `error` や nil pointer を使わない。
- Step は準純粋関数として Workflow から直接呼ぶ。Step に外部 client を注入しない。
- OS Artifact は digest 固定参照を使用し、可変 tag を受け入れない。
- Secret、Bootstrap Data、Session Token、署名鍵を Resource Status、ログ、テスト出力へ出さない。
- 長時間処理の再開に必要な状態は `TartHostOperation` へ保存し、process 内メモリを正本にしない。

CRD、Webhook、Controller の変更には Kubebuilder または controller-gen を使用する。生成手順は
[開発ガイド](development.md)を参照する。
