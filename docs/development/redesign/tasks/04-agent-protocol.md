# Task 04: Agent Protocolと認証

## 目的

Provisioning AgentとNode Lifecycle Serviceが、同じOperationを重複実行せず、TokenまたはBootstrap Dataをlogへ漏らさずにPlan/Bundleを取得・報告できるProtocolを実装する。

## 依存

- Task 02のOperation schema
- Task 05のManifest schema
- ADR 0004、0006、0011

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

## 実装状況

2026-07-05時点で、Task 02とTask 05 Manifest schemaに依存するProtocolの縦方向実装を
先行した。`--agent-api-bind-address`の既定値は`0`であり、TLS certificate/keyと
`--agent-api-allow-isolated-l2`を明示した場合だけproduction listenerを起動する。

| 受け入れ条件 | 状況 | 証跡または残作業 |
|---|---|---|
| 1 | 実装済み | `TestServiceAllowsOneOfOneHundredConcurrentBootstrapClaims`でKubernetes StatusのresourceVersion競合を使い100並列中1件だけclaim |
| 2 | 実装済み | Host UID、Operation UID、expiryの拒否をDomain testで確認し、`TestHandlerRejectsInvalidSessionOnEveryProtectedEndpoint`と`TestHandlerRejectsExpiredSession`でHTTPS endpoint境界も確認 |
| 3 | 実装済み | `TestServiceLocksAfterFiveFailures` |
| 4 | 実装済み | token hash、expiry、失敗回数、消費状態を`TartHostOperation.status`へ保存し、`TestServicePersistsAndRestoresSession`でService再生成後の認証を確認 |
| 5 | 実装済み | `TestHandlerProgressSequence`と`TestServiceAppliesSequenceOneTwoTwoOneFourThree` |
| 6 | 実装済み | `TestHandlerRejectsRequestLargerThanOneMiB`、`TestHandlerRejectsUnsupportedFormatAndOversizedBootstrap` |
| 7 | 実装済み | Agent APIはaccess log middleware、Event、独自trace attributeを生成しない。`TestHandlerErrorResponseDoesNotReflectCredentialOrRequestValue`と`TestServicePersistsAndRestoresSession`でerror responseとStatusに平文Token/request値がないことを確認 |
| 8 | 実装済み | `cloud-config`以外を422で拒否し、Ignition adapterは実装しない |
| 9 | 実装済み | [ADR 0011](../adr/0011-initial-agent-credential.md)。iPXE-only MVPはInitial secretを配らず隔離L2とTLS pinningを必須にする |
| 10 | 実装済み | `TartHost.status.conditions`へ`CredentialRequirement=True`、`Reason=IsolatedL2Required`として表示し、既存の`Degraded` conditionを壊さないことをtestで固定した |

実装済みの主な構成:

- `dto/agent`: request/response、Plan、Bootstrap Bundle、RFC 8785 canonicalization、Ed25519署名
- `artifact/schema`: PlanとBootstrap BundleのJSON Schema
- `domain/shared/agentsession`: 256 bit token、binding、TTL、失敗上限、消費状態
- `domain/shared/agentprogress`: sequenceの適用、重複、gap判定
- `infrastructure/repository/k8s/agentapi`: Operation lookup、署名済みPlan Secret、CABPK Bootstrap Secretの読取り
- `domain/shared/bootreport`: boot完了条件と冪等なPhase遷移の純粋判定
- `infrastructure/repository/k8s/bootreport`: 最新boot reportのStatus永続化と`BootTrial`から`AwaitingHealth`への遷移
- `infrastructure/http_server/agentapi`: TLS 1.3 listener、rate limiter、`/v1` endpoint

boot reportは失敗したmount結果も`status.lastBootReport`へ保存する。Planが対象にするOS slot、
Artifact Generation、State/Data mount、Bootstrap成功markerが全て一致した場合だけ
`AwaitingHealth`へ進む。同一reportの再送はStatusを更新せず、完了後に異なるboot IDまたは
内容を送った場合は409を返す。Node Ready等のHealth Gate判定はTask 07が引き継ぐ。
OS health serviceは再起動後にregisterを再実行して新しいSession Tokenを取得する。
Bootstrap claimで以前のTokenは失効するが、`status.bootstrapDelivered`はOperation単位で
保持するため、新Sessionを使ったBootstrap Dataの再取得は拒否する。

progress endpointは署名済みPlanを再読込みし、Plan Digestと`completedStep`の所属を検証する。
PlanにないStep名はStatusへ保存せず422を返す。

Task 06のdisk安全判定に必要なため、Planは`operationType`、Update時の`activeSlot`、
`rootDevice.deviceName`を必須入力として持つ。`deviceName`は`/dev/disk/by-id/`だけを受理し、
これらを含むCanonical JSONを署名・digest対象にする。

残作業:

- Plan SecretをOperation作成時に生成するTask 07側の接続
- `MutualTLS`、`SignedChallenge`、`BMCProtectedMedia`のRegistration Verifier

## 対象外

- disk処理
- 任意remote shell
- Ignition
- 任意cloud-init customization

## 関連

- ADR 0004、0006、0011
- Issue #147
