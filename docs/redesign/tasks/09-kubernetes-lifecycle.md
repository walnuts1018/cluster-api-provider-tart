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
5. 旧slot稼働中に、新OS slot（Inactive Slot）のパーティションを一時的に読み取り専用でマウントし、そこから target kubeadm バイナリを実行することで `kubeadm upgrade apply` を実行する。
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

## 実装状況（2026-07-08）

Task08の実機/E2E未検証前提を維持したまま、Distribution LifecycleのI/Oへ依存しない
判定ロジックから先行実装を開始した。

実装済み:

- `internal/domain/distributionlifecycle`に、KubernetesBinary/StateMigration向けの
  Preflight純粋判定を追加
- minor versionを2つ以上進める更新、downgrade、major version更新、不正なversionを拒否する判定
- worker更新でcontrol planeがtarget versionを受理していない場合に拒否する判定
- StateMigrationでSnapshotRefなしにLifecycle stepを開始しない判定
- Snapshot作成前にもcontrol plane Planを生成し、`KubeadmApplied`直前にSnapshotRefを必須にする判定
- 7つの永続化Stepを順序通りに1回だけ記録し、同じStepの再報告を冪等に扱う純粋ロジック
- Status上の`completedSteps`がPlan順序と異なる場合に拒否するapplication層の検証
- workerではSnapshotなし、control planeではSnapshotを`KubeadmApplied`前に含める
  Distribution Lifecycle Plan順序の純粋生成
- `DistributionLifecycleDriver` Portと、Preflight/Snapshot/Apply/Verifyを任意commandではなく
  型付きStepとしてdispatchするapplication service
- Snapshot作成結果でrestore test成功を必須にし、失敗したSnapshotRefを使用しない判定
- `TartHostOperation.status.completedSteps`、`lifecyclePhase`、`snapshotRef`へLifecycle Step結果を保存する
  Kubernetes adapter
- StateMigration失敗時に`RecoveryRequired`へ遷移し、既存`SnapshotRef`を保持するStatus更新
- Node Lifecycle Serviceの失敗報告時、`KubernetesBinary`は`RollingBack`へ遷移して旧slot復帰へ進み、
  `StateMigration`だけを`RecoveryRequired`へ遷移させるStatus更新
- `cmd/node-lifecycle-service` に、署名済みPlanの`deadline`までstep実行と
  `node-lifecycle-progress`再送を継続する再接続制御を追加し、
  temporaryなmanagement API停止では即終了しないようにした
- Node Ready、期待version、static Pod、etcd quorum、API healthをCommit前に評価するHealth Gate純粋判定
- `UpgradePlan`、`SaveEtcdSnapshot`、`VerifyEtcdSnapshot`、`UpgradeApply`、`UpgradeNode`、
  `ObserveHealth`の型付きRuntimeへdispatchするkubeadm Lifecycle Driver
- control planeの`ObserveHealth`で、Node Ready/versionに加えて
  static Pod readiness、`etcdctl endpoint health --cluster`、`kubectl get --raw=/readyz`
  を個別観測するkubeadm Runtime
- Distribution Lifecycle用feature gateをworker、複数control plane、単一control planeの順で有効化する純粋判定
- Node Lifecycle Service境界で受け取るPlanをEd25519署名対象のCanonical JSONとし、
  未署名・改ざん済みPlanを既存のDistribution Lifecycle Step runnerへ渡さない検証
- `cmd/node-lifecycle-service`を追加し、Agent APIから取得した署名済みNode Lifecycle Planを検証して、
  指定されたLifecycle Stepだけをkubeadm Driverへdispatchするprocess境界を作成
- Node Lifecycle Service用のkubeadm Runtimeを追加し、`kubeadm upgrade plan/apply/node`、
  `etcdctl snapshot save/status`、`kubectl get node`を任意shell commandではなく型付き操作として実行
- Domain PlanからNode Lifecycle Service向け署名済みPlanとPlan Digestを生成するapplication境界
- 署名済みNode Lifecycle PlanをTartHostOperation所有のimmutable Secretへ冪等保存するKubernetes adapter
- 保存済みNode Lifecycle Plan SecretからPlanと署名を復元するKubernetes provider
- controllerのAgent APIへ`/v1/operations/{uid}/node-lifecycle-plan`を追加し、Session Token認証、
  OperationのPlanDigest照合、Kubernetes Secret providerからの署名済みPlan配信を接続
- Provisioning Agent clientへNode Lifecycle Plan取得APIを追加し、PlanDigestとEd25519署名を検証
- Node Lifecycle ServiceのStep成功/失敗をSession Token認証済みAgent API
  `/v1/operations/{uid}/node-lifecycle-progress`へ報告し、成功StepをStatusStoreの`completedSteps`、
  失敗Stepを`RecoveryRequired`遷移へ接続
- KubernetesBinary Update Operation作成時にworker/control plane別Node Lifecycle Planを生成し、
  `TartHostOperation.spec.nodeLifecyclePlanDigest`とOperation所有のimmutable Secretへ保存する接続
- `TartMachine.status.installedDistributionVersion`を追加し、Update成功後の次回更新でcurrent version入力として使用
- Distribution Lifecycle feature gateが有効な対象に限り、`CanUpdateMachine`/`CanUpdateMachineSet`
  がKubernetes version差分をin-place update対象として受理し、無効時はpatchなしで通常置換へfallbackする
- `docs/redesign/runbooks/09-kubernetes-lifecycle-recovery.md` に
  `RecoveryRequired` と `SnapshotRef` を前提にした手動復旧 Runbook を追加
- `internal/adapter/k8s/distributionlifecycle/status_store_test.go` に、
  7つの永続化Stepごとにfresh processから同じ完了報告を再送しても`completedSteps`を重複記録しない
  回帰テストを追加
- `internal/server/agentapi/handler_test.go` に、
  7つのNode Lifecycle Step成功報告をfresh handlerごとに再送し、
  `completedSteps`、`lifecyclePhase`、`snapshotRef`、`phase`が再起動後も同じStatusへ収束する
  統合テストを追加
- `internal/server/agentapi/handler_test.go` に、
  `StateMigration` が `KubeadmApplied` で失敗した時に `RecoveryRequired` と `SnapshotRef` 保持へ
  収束する統合テストを追加
- `docs/redesign/runbooks/09-kubernetes-lifecycle-simulated-record.md` に、
  repository 内で再現可能な再起動耐性と `RecoveryRequired` の証跡を追加

未実装・未検証:

- 7つの各Step直後のcontrollerまたはNode再起動の実機/E2E検証
- Recovery Runbookの実機/E2E実行記録
