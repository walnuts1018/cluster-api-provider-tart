# Task 04: Agent Protocolと認証

## 目的

Provisioning AgentとNode Lifecycle Serviceが、同じOperationを重複実行せず、TokenまたはBootstrap Dataをlogへ漏らさずにPlan/Bundleを取得・報告できるProtocolを実装する。

## 依存

- Task 02のOperation schema
- Task 05のManifest schema
- ADR 0004、0006

## 入力

- `TartHost`のHost UID、hardware identity、Platform Profile
- `TartHostOperation`のOperation UID、Plan Digest、deadline
- Bootstrap Secretの`value`と`format`
- Artifact Manifestのdigest、generation、署名検証結果

## Protocol endpoint

| Method/Path | Client | 用途 | 冪等性key |
|---|---|---|---|
| `POST /v1/agent/register` | Provisioning Agent | Host/Operation認証とinventory送信 | Operation UID + Agent instance ID |
| `GET /v1/operations/{uid}/plan` | Agent/Node Lifecycle Service | 署名済みPlan取得 | Operation UID + Plan Digest |
| `POST /v1/operations/{uid}/progress` | Agent/Node Lifecycle Service | Phase/Step報告 | Operation UID + agentSequence |
| `GET /v1/operations/{uid}/bootstrap` | Provisioning Agent | Bootstrap Bundleの単回取得 | Session Token |
| `POST /v1/operations/{uid}/boot-report` | OS health service | slot/generation/mount結果報告 | Operation UID + boot ID |

全endpointはHTTPSだけをlistenし、HTTPからのredirectは提供しない。

## 成果物

- request/responseのGo型とOpenAPIまたはJSON Schema
- canonical Plan serialization
- Session Token発行・hash保存・失効service
- Bootstrap Bundle schema
- progress sequence検証
- rate limiter
- Token/Secretを除外するlog/trace filter
- Initial Credential方式を決めるADRまたは既存ADR更新

## Protocol要件

- Request body上限は1 MiB、Bootstrap response上限は16 MiBとする。
- Session TokenはAuthorization headerへ入れ、URLへ入れない。
- Session Tokenは発行から10分、認証失敗5回、Bundle response送信完了のいずれかで失効する。
- `agentSequence`は1から開始し、保存値より1大きい値だけ状態を進める。
- 保存値以下は200と現在Statusを返す。2以上先の値は409を返す。
- Operation UIDまたはPlan Digestが不一致なら404の同一error bodyを返し、存在有無を区別させない。
- Bootstrap Bundleは`apiVersion`、`format`、`payload`、`payloadDigest`、`machineUID`、`operationUID`を必須とする。

## 受け入れ条件

1. 同じTokenを100並列で使用し、Bootstrap取得成功が1件だけになる。
2. 別Host UID、別Operation UID、期限後のTokenを全て401で拒否する。
3. 認証失敗5回後、正しいTokenでも401になる。
4. controller再起動後もToken hash、expiry、Operation Statusを復元する。
5. sequence 1、2、2、1、4、3を送信した場合、1/2/3だけを順に適用し、4を409にする。
6. 1 MiB超requestと16 MiB超Bundleを413で拒否する。
7. log、Event、Status、trace dumpにToken、payload、Secret valueが含まれない。
8. unsupported `format`をdisk書き込み前に422で拒否する。
9. Initial CredentialがURL query、公開iPXE script、kernel command line、access logへ現れない。
10. hardware identityなしのProfileが隔離L2要件をStatus/利用者文書へ表示する。

## 完了証跡

- Protocol schema
- 100並列Token test結果
- controller再起動test
- sequence test
- sanitized log/trace sample
- Initial Credential threat model

## 対象外

- disk処理
- 任意remote shell
- Ignition
- 任意cloud-init customization

## 関連

- ADR 0004、0006
- Issue #147
