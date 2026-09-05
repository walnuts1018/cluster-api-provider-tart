# old/ — 参考用に退避した旧実装

このディレクトリは、Talos専用のCluster API Provider設計への作り直しにあたり、
新設計では直接使わないが実装の参考になりそうなコードを退避したものである。
`old/go.mod`によって独立したGo moduleにしてあり、リポジトリ本体の
`go build ./...`、`go vet ./...`、`golangci-lint run`、`go test ./...`の対象には含まれない。
このディレクトリのコードはコンパイルが通ることを保証しない。

新設計の実装時は、対応する各ディレクトリの`README.md`で「何が参考になるか」を確認してから
必要な部分だけを新しいAPI型・package構成に合わせて書き直すこと。そのままコピーして
新packageへ持ち込まない。

## 一覧

| ディレクトリ | 由来 | 参考にする理由 |
| --- | --- | --- |
| `host-power-backend/` | `infrastructure/service/driver/*`, `redfish/`, `wol/`, `fake/` | Wake-on-LAN/Redfish/manualをcapability interfaceで抽象化する設計。`boot`/`host` package実装の参考。 |
| `host-claim-cas/` | `infrastructure/repository/k8s/allocation/service.go` | resourceVersion付きUpdateによるoptimistic concurrency claimの実装例。`TartHost.spec.consumerRef`のatomic CAS実装の参考。 |
| `runtime-extension-wire/` | `infrastructure/http_server/extension/*` | CAPI Runtime ExtensionのHTTPハンドラ登録・JSON Patch生成部分。判定ロジック自体はAgent Plan前提で使えない。 |
| `node-quiesce-decision/` | `domain/node/entity/nodelifecycleengine/*` | drain安全性judgeを純粋関数で分割している構成。multi-node drain必須/single-node best-effort判断の参考。 |
| `resource-finalizer-runner/` | `infrastructure/service/resource_finalizer/*` | finalizer stepを小さく合成するrunnerパターン。 |
| `admission-validation/` | `infrastructure/k8s_webhook/v1beta1/validation.go` | immutable field変更拒否などのvalidation構成パターン。 |

その他のディレクトリ(`domain/shared/*`の一部、`infrastructure/k8s_controller/`、
`infrastructure/service/driver/`、`infrastructure/service/wol/`、
`infrastructure/http_server/bootstrapper/`、`test/e2e/provisioning/`、
`config/extension/`など)は、他エージェントの並行作業によりこの`old/`へ
機械的に退避されたものを引き継いでいる。個別の参考価値は上表ほど高くないため、
利用前に内容を確認すること。
