# Task 09: Kubernetes Distribution Lifecycle

## 目的

A/B OS slot更新とは別に、既存Node上でkubeadmのversion更新、Snapshot、検証、Recoveryを実行する。

## 依存

- Task 08
- ADR 0008

## 入力

- CAPI rollout ownerが指定したcurrent/target Kubernetes version
- Update class
- Plan Digest
- desired Machine/BootstrapConfig digest
- Active/target Slot
- State schema

## 成果物

- `DistributionLifecycleDriver` Port
- kubeadm Adapter
- 署名済みPlanだけを実行するNode Lifecycle Service
- worker/control plane別Plan
- SnapshotRef
- Lifecycle Phase/Stepを持つOperation Status
- Recovery Runbook

## worker更新順

1. control planeがtarget versionを受理済みであることを検証する。
2. target OS Slotを書き込んでverifyする。
3. target Slotをbootするがkubeletを起動しない。
4. State/Dataをmountする。
5. target kubeadmで`kubeadm upgrade node`を実行する。
6. kubeletを起動する。
7. Node Readyと期待versionを検証する。
8. SlotをCommitする。

## control plane更新順

1. CAPI rollout ownerが当該Nodeの更新を許可したことを検証する。
2. version skew、etcd quorum、disk空き容量をPreflightする。
3. etcd Snapshotを作成し、SnapshotRefと復元検証結果を保存する。
4. target OS Slotを書き込んでverifyする。
5. 旧slot稼働中にtarget kubeadmで`kubeadm upgrade apply`を実行する。
6. target Slotをbootする。
7. Node Ready、static Pod、etcd quorum、API healthを検証する。
8. SlotとLifecycle GenerationをCommitする。

## 永続化するStep

- `PreflightCompleted`
- `SnapshotCreated`
- `TargetSlotWritten`
- `KubeadmApplied`
- `TargetSlotBooted`
- `HealthVerified`
- `Committed`

各Step成功直後にOperation Statusを更新する。Status更新前にprocessが終了した場合、再実行しても同じ結果へ収束する実装だけを許可する。

## 受け入れ条件

1. Distribution Lifecycle未実装時にKubernetes version差分をin-place patchで覆わない。
2. minor versionを1つ以上skipするPlanをPreflightで拒否する。
3. workerをcontrol planeより先にtarget versionへ更新しない。
4. control planeでSnapshotRefなしに`kubeadm upgrade apply`を実行しない。
5. Snapshot作成後にrestore testを実行し、失敗したSnapshotを使用しない。
6. 7つの各Step直後にcontrollerまたはNodeを再起動し、Stepを重複適用しない。
7. Node Ready、期待version、static Pod、etcd quorumのいずれかが失敗した場合にCommitしない。
8. OS slotだけ戻してStateMigrationを`Succeeded`と報告しない。
9. StateMigration失敗時にOperation=`RecoveryRequired`、SnapshotRef保持となる。
10. worker、3台control plane、単一control planeの順にfeature gateを有効化する。
11. 単一control planeではmanagement API停止中のcontroller再接続を含むE2Eが成功するまでExperimentalとする。

## 完了証跡

- worker/control plane Plan例
- version skip拒否test
- Snapshot作成/restore test
- 7再起動pointのOperation Status
- etcd quorum/API health log
- Recovery Runbook実行記録

## 対象外

- 任意command実行API
- package managerによる任意version更新
- application/PVの整合性Snapshot
- k3s実装

## 関連

- ADR 0002、0008
