# Task 09: Kubernetes Node Lifecycle Engine Simulated Record

## 目的

Task 09 の再起動耐性と `RecoveryRequired` 収束について、repository 内で再現可能な統合テスト証跡を残す。
この記録は実機/E2E の代替ではない。`infrastructure/http_server/agentapi/handler_test.go` で fake Kubernetes client を使い、
各 request ごとに fresh `Handler` を作り直すことで controller 再起動相当を確認した結果である。

## 実行コマンド

```bash
go test ./infrastructure/http_server/agentapi -v
go test ./cmd/node-lifecycle-service -v
```

確認対象:

- `TestHandlerは各NodeLifecycleStep直後の再起動後も完了報告を重複記録しない`
- `TestHandlerは一時停止復帰後もfreshHandler経由で完了Stepを一度だけ保存する` は fake Kubernetes client 上の `TartHostOperation.status` を正本にして、最初の 3 回の `POST /node-lifecycle-progress` が `503` でも、各 request ごとに fresh `Handler` を作り直した復帰後 request で `completedSteps` が 1 回だけ増えることを確認する
- `TestHandlerはStateMigration失敗時にSnapshotRefを保持したままRecoveryRequiredへ遷移する`
- `TestFetchNodeLifecyclePlanWithRetryRecoversAfterInnerRetriesExhausted` は management API が最初の 3 request で `503` を返しても、node-lifecycle-service 外側 retry で 4 回目の Plan 取得へ復帰できることを確認する
- `TestNodeLifecycleServiceRecoversTemporaryOutageAcrossPlanFetchAndProgressReport` は real `agentclient` と local TLS test server を使い、最初の 3 回の `GET /node-lifecycle-plan` と後段の最初の 3 回の `POST /node-lifecycle-progress` がそれぞれ `503` でも、同一 service 実行フローが成功まで進むことを確認する
- `TestReportStepOutcomeWithRetryRecoversAfterInnerRetriesExhausted` は management API が最初の 3 request で `503` を返しても、node-lifecycle-service 外側 retry で 4 回目に `204` へ復帰できることを確認する

## シナリオ 1: 7 Step 成功 + 各 Step の重複再送

前提:

- control plane 向け `KubernetesBinary` Plan
- 各 Step 成功直後に同じ request をもう一度送信する
- 2 回目は fresh `Handler` で処理し、process memory に依存しないことを確認する

期待した `TartHostOperation.status`:

| Step | lifecyclePhase | phase | snapshotRef | completedSteps |
|---|---|---|---|---|
| `PreflightCompleted` | `Preflight` | `DistributionUpdating` | なし | `["PreflightCompleted"]` |
| `SnapshotCreated` | `Snapshot` | `DistributionUpdating` | `etcd-snapshot-1` | `["PreflightCompleted","SnapshotCreated"]` |
| `TargetSlotWritten` | `Apply` | `DistributionUpdating` | `etcd-snapshot-1` | `["PreflightCompleted","SnapshotCreated","TargetSlotWritten"]` |
| `DistributionApplied` | `Apply` | `DistributionUpdating` | `etcd-snapshot-1` | `["PreflightCompleted","SnapshotCreated","TargetSlotWritten","DistributionApplied"]` |
| `TargetSlotBooted` | `Apply` | `DistributionUpdating` | `etcd-snapshot-1` | `["PreflightCompleted","SnapshotCreated","TargetSlotWritten","DistributionApplied","TargetSlotBooted"]` |
| `HealthVerified` | `Verify` | `DistributionUpdating` | `etcd-snapshot-1` | `["PreflightCompleted","SnapshotCreated","TargetSlotWritten","DistributionApplied","TargetSlotBooted","HealthVerified"]` |
| `Committed` | `Apply` | `Succeeded` | `etcd-snapshot-1` | `["PreflightCompleted","SnapshotCreated","TargetSlotWritten","DistributionApplied","TargetSlotBooted","HealthVerified","Committed"]` |

判定:

- duplicate report はすべて `204 No Content`
- `completedSteps` は 2 回目の送信で増えない
- `SnapshotCreated` 後の `snapshotRef` は後続 Step でも保持される

## シナリオ 2: StateMigration 失敗時の `RecoveryRequired`

前提:

- control plane 向け `StateMigration` Plan
- `PreflightCompleted`、`SnapshotCreated` 成功後に `DistributionApplied` を失敗報告する

期待した `TartHostOperation.status`:

- `phase=RecoveryRequired`
- `snapshotRef.name=etcd-snapshot-1`
- `completedSteps=["PreflightCompleted","SnapshotCreated"]`

判定:

- 失敗報告後も `SnapshotRef` は失われない
- 自動で `Succeeded` や `Failed` へ進まず、Runbook 前提どおり `RecoveryRequired` に停止する

## シナリオ 3: temporary outage 復帰後の fresh Handler 再受理

前提:

- control plane 向け `KubernetesBinary` Plan
- 最初の 3 回の `POST /node-lifecycle-progress` は management API 停止相当として `503`
- 4 回目以降は毎回 fresh `Handler` へ委譲し、controller restart 相当でも process memory に依存しないことを確認する

期待した `TartHostOperation.status`:

- outage 中は `completedSteps=[]`
- 復帰直後の `PreflightCompleted` 成功で `completedSteps=["PreflightCompleted"]`
- 同じ request の重複再送後も `completedSteps` は増えない

判定:

- outage 応答では Kubernetes 上の Status が変化しない
- 復帰後の最初の成功 request だけが `completedSteps` を 1 回追加する
- 重複再送は `204 No Content` を返しても `completedSteps` を再追加しない

## 残っていること

- Node 自体の再起動
- 実 controller Pod の再起動
- Plan 初回取得と Step 完了報告の両方で temporary outage 吸収を repository test では確認済みだが、management API 停止を含む単一 control plane 実機/E2E 検証
- Runbook の実機実行記録

これらは fake client では代替できないため、GitHub Actions または実機環境で別途 E2E を実行する。
