# Task 09: Kubernetes Distribution Lifecycle Simulated Record

## 目的

Task 09 の再起動耐性と `RecoveryRequired` 収束について、repository 内で再現可能な統合テスト証跡を残す。
この記録は実機/E2E の代替ではない。`internal/server/agentapi/handler_test.go` で fake Kubernetes client を使い、
各 request ごとに fresh `Handler` を作り直すことで controller 再起動相当を確認した結果である。

## 実行コマンド

```bash
go test ./internal/server/agentapi -v
```

確認対象:

- `TestHandlerは各NodeLifecycleStep直後の再起動後も完了報告を重複記録しない`
- `TestHandlerはStateMigration失敗時にSnapshotRefを保持したままRecoveryRequiredへ遷移する`

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
| `KubeadmApplied` | `Apply` | `DistributionUpdating` | `etcd-snapshot-1` | `["PreflightCompleted","SnapshotCreated","TargetSlotWritten","KubeadmApplied"]` |
| `TargetSlotBooted` | `Apply` | `DistributionUpdating` | `etcd-snapshot-1` | `["PreflightCompleted","SnapshotCreated","TargetSlotWritten","KubeadmApplied","TargetSlotBooted"]` |
| `HealthVerified` | `Verify` | `DistributionUpdating` | `etcd-snapshot-1` | `["PreflightCompleted","SnapshotCreated","TargetSlotWritten","KubeadmApplied","TargetSlotBooted","HealthVerified"]` |
| `Committed` | `Apply` | `Succeeded` | `etcd-snapshot-1` | `["PreflightCompleted","SnapshotCreated","TargetSlotWritten","KubeadmApplied","TargetSlotBooted","HealthVerified","Committed"]` |

判定:

- duplicate report はすべて `204 No Content`
- `completedSteps` は 2 回目の送信で増えない
- `SnapshotCreated` 後の `snapshotRef` は後続 Step でも保持される

## シナリオ 2: StateMigration 失敗時の `RecoveryRequired`

前提:

- control plane 向け `StateMigration` Plan
- `PreflightCompleted`、`SnapshotCreated` 成功後に `KubeadmApplied` を失敗報告する

期待した `TartHostOperation.status`:

- `phase=RecoveryRequired`
- `snapshotRef.name=etcd-snapshot-1`
- `completedSteps=["PreflightCompleted","SnapshotCreated"]`

判定:

- 失敗報告後も `SnapshotRef` は失われない
- 自動で `Succeeded` や `Failed` へ進まず、Runbook 前提どおり `RecoveryRequired` に停止する

## 残っていること

- Node 自体の再起動
- 実 controller Pod の再起動
- management API 停止を含む単一 control plane 検証
- Runbook の実機実行記録

これらは fake client では代替できないため、GitHub Actions または実機環境で別途 E2E を実行する。
