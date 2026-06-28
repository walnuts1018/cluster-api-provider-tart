# タスク一覧

## 1. タスクの読み方

全タスクの用語と要求レベルは[記述規約と用語集](../conventions.md)に従う。タスク文書の各節は次の意味を持つ。

| 節 | 意味 |
|---|---|
| 目的 | タスク完了後に新しく可能になること |
| 依存 | 開始前に完了が必須のTask/ADR |
| 入力 | 実装が読み取るAPI、Artifact、Protocol |
| 成果物 | repositoryへ追加・変更するcode、schema、文書 |
| 実装要件 | 成果物が必ず満たす動作 |
| 受け入れ条件 | Yes/Noで完了を判定するtest |
| 完了証跡 | PRまたはIssueへ添付するcommand出力、Artifact、実機情報 |
| 対象外 | このTaskで実装してはならない事項 |

「実装した」だけではタスク完了にしない。全受け入れ条件に対応するtest名または検証記録をIssueへ記載する。

## 2. タスク一覧

| # | タスク | 主な成果 | 依存 |
|---|---|---|---|
| 01 | [基礎検証とADR確定](01-foundation-spikes.md) | CAPI Hook、A/B、read-only root、Rollbackの検証記録 | なし |
| 02 | [APIとDomain再設計](02-api-and-domain.md) | CRD、Webhook、状態機械、排他割当 | 01 |
| 03 | [Driver抽象化とWoL移行](03-driver-abstraction.md) | Port、Capability、Driver Registry、WoL Adapter | 01、02の型定義 |
| 04 | [Agent Protocolと認証](04-agent-protocol.md) | `/v1` schema、Session Token、Bootstrap Bundle | 02、05のManifest schema |
| 05 | [OS Artifact Build](05-image-pipeline.md) | OS/Verity Artifact、Manifest、SBOM、署名 | 01 |
| 06 | [AgentとNetwork Boot統合](06-network-boot-agent.md) | 一時OS起動、disk inventory/write/verify | 03、04、05 |
| 07 | [初期Provisioning](07-initial-provisioning.md) | Ubuntu 24.04 kubeadm Node Ready | 02、06 |
| 08 | [OSOnly A/B更新](08-ab-update.md) | Runtime Hook、boot trial、Rollback | 07 |
| 09 | [Kubernetes Distribution Lifecycle](09-kubernetes-lifecycle.md) | kubeadm update、Snapshot、Recovery | 08 |
| 10 | [Redfish](10-redfish.md) | Power、Boot Transport、Virtual Media | 03、06 |
| 11 | [対応Matrix拡大とRelease](11-compatibility-and-release.md) | OS/architecture/firmware追加、Runbook | 09、10 |

## 3. GitHub Issue運用

- このMarkdownは設計上の分割を表す。進捗の正本はGitHub Issueとする。
- Task開始時に1つの親Issueを作成する。
- 1 PRで完了しないTaskは、受け入れ条件の集合ごとにsub-issueを作成する。
- sub-issueは1つ以上の受け入れ条件へ対応し、対応しない作業を含めない。
- PR本文には検証した受け入れ条件の番号とcommandを記載する。
- PRは対象Issueを`Closes #...`で関連付ける。
- 設計変更を伴うPRはADRとTask文書を同じcommitまたは直前commitで更新する。

## 4. 共通Definition of Done

### Code

- Reconcileの判断はprocess memoryだけへ保存しない。
- 外部APIにはcontext deadlineを渡す。
- Retryは[共通上限値](../conventions.md#11-時間回数上限値)に従う。
- Condition、Event、log messageは英語とする。
- Token、Secret、Bootstrap payloadをlog、Event、Status、traceへ出さない。
- Tracer/Meterは`telemetry.Tracer`等のglobal providerから取得する。
- CRD/WebhookはKubebuilder/controller-genで生成する。

### Test

- Domain状態遷移はtable testを持つ。
- Race条件は並列testまたはenvtestで再現する。
- Error分類ごとに少なくとも1つのtestを持つ。
- application logic以外の「fileが存在する」だけのtestを追加しない。
- `mise run test-e2e`と`mise run test-provisioning-e2e`をlocalで実行しない。

### Evidence

- Go testはpackageとtest名が分かるcommand出力を保存する。
- QEMU testはCPU model、firmware、disk size、Artifact digestを保存する。
- 実機testは機種、Firmware version、NIC/storage controller、BMC versionを保存する。
- failure injectionは注入位置、期待状態、実際の最終状態を保存する。

## 5. Taskを分割し直す条件

次のいずれかに該当するTaskは、実装開始前にsub-issueへ分割する。

- 2つ以上の独立したCRDを追加する。
- 2つ以上のOS/architecture/firmware組合せを追加する。
- 実機設備が異なる検証を含む。
- 1つの受け入れ条件が別の受け入れ条件なしでrelease可能である。
