# Task 08 OSOnly A/B Update Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 同じMachine、Host、Node identityを保ったままOS ArtifactをInactive Slotへ書き込み、失敗時に旧slotへ自動RollbackするExperimentalなOSOnlyインプレース更新を実装する。

**Architecture:** Runtime ExtensionはCAPI request/responseの変換だけを担当し、差分分類と更新状態遷移は副作用のないdomainへ分離する。application serviceはdesired object digestを冪等性キーとして既存Operationを再利用し、Kubernetes adapterがOperation、Host、TartMachineの状態を永続化する。Task 01未検証のbootloader挙動はPortの背後に置き、RuntimeSDK/InPlaceUpdatesを既定無効にする。

**Tech Stack:** Go、Cluster API Runtime SDK v1alpha1 hooks、Cluster API core/v1beta2、controller-runtime、Kubernetes SSA、RFC 8785 Canonical JSON、Ed25519、OpenTelemetry。

---

### Task 1: OSOnly差分を純粋関数で分類する

**Files:**
- Create: `domain/provisioning/entity/inplaceupdate/change.go`
- Test: `domain/provisioning/entity/inplaceupdate/change_test.go`
- Delete: `infrastructure/http_server/extension/patch_test.go`

- [x] **Step 1: 許可・拒否差分の失敗テストを書く**

`ChangeSet`へcurrent/desiredのMachine、TartMachine、BootstrapConfigを渡し、次の表をtable testにする。

```go
tests := []struct {
    name    string
    mutate  func(*ChangeSet)
    allowed bool
    paths   []FieldPath
}{
    {name: "image ref", mutate: changeImageRef, allowed: true, paths: []FieldPath{FieldTartMachineImageRef}},
    {name: "update policy", mutate: changeUpdatePolicy, allowed: true, paths: []FieldPath{FieldTartMachineUpdatePolicy}},
    {name: "Kubernetes version", mutate: changeMachineVersion, allowed: false, paths: []FieldPath{FieldMachineVersion}},
    {name: "bootstrap payload", mutate: changeBootstrap, allowed: false, paths: []FieldPath{FieldBootstrapConfig}},
    {name: "platform profile", mutate: changePlatformProfile, allowed: false, paths: []FieldPath{FieldTartMachinePlatformProfile}},
    {name: "host selector", mutate: changeHostSelector, allowed: false, paths: []FieldPath{FieldTartMachineHostSelector}},
    {name: "provider ID", mutate: changeProviderID, allowed: false, paths: []FieldPath{FieldTartMachineProviderID}},
    {name: "deletion policy", mutate: changeDeletionPolicy, allowed: false, paths: []FieldPath{FieldTartMachineDeletionPolicy}},
}
```

- [x] **Step 2: テストが未実装で失敗することを確認する**

Run: `MISE_OFFLINE=1 mise exec -- go test ./domain/provisioning/entity/inplaceupdate -run TestClassify -v`

Expected: `package .../domain/provisioning/entity/inplaceupdate is not in std`または未定義symbolでFAIL。

- [x] **Step 3: 閉じたFieldPathと分類結果を実装する**

```go
type Classification struct {
    Changed []FieldPath
    Allowed []FieldPath
    Rejected []FieldPath
}

func (c Classification) CanUpdateInPlace() bool {
    return len(c.Changed) > 0 && len(c.Rejected) == 0
}

func Classify(current, desired ChangeSet) Classification
```

`reflect.DeepEqual`ではなくKubernetes semantic equalityを使い、BootstrapConfigはCanonical JSON化した`spec`だけを比較する。metadata/status差分は分類対象に含めない。

- [x] **Step 4: domain testを通す**

Run: `MISE_OFFLINE=1 mise exec -- go test ./domain/provisioning/entity/inplaceupdate -v`

Expected: PASS。

- [x] **Step 5: 旧v1alpha1差分テストを削除してコミットする**

```bash
git add domain/provisioning/entity/inplaceupdate infrastructure/http_server/extension/patch_test.go
git commit --signoff -m "feat: OSOnly更新差分を分類する"
```

### Task 2: CanUpdate hooksをv1beta1のallowlistへ置き換える

**Files:**
- Modify: `infrastructure/http_server/extension/patch.go`
- Modify: `infrastructure/http_server/extension/canupdatemachine.go`
- Modify: `infrastructure/http_server/extension/canupdatemachineset.go`
- Create: `infrastructure/http_server/extension/canupdatemachine_test.go`
- Create: `infrastructure/http_server/extension/canupdatemachineset_test.go`

- [x] **Step 1: 6 patch allow/deny tableのHook失敗テストを書く**

Machine、InfraMachine、BootstrapConfigのcurrent/desired RawExtensionを組み立て、許可差分だけを対応するJSON Merge Patchで覆い、拒否差分がある場合はpatchを返さないことを検証する。

```go
HandleCanUpdateMachine(ctx, request, response)
if wantPatch {
    require.Equal(t, runtimehooksv1.ResponseStatusSuccess, response.Status)
    require.True(t, response.InfrastructureMachinePatch.IsDefined())
} else {
    require.False(t, response.InfrastructureMachinePatch.IsDefined())
}
```

- [x] **Step 2: 旧実装に対して失敗することを確認する**

Run: `MISE_OFFLINE=1 mise exec -- go test ./infrastructure/http_server/extension -run 'TestHandleCanUpdate' -v`

Expected: v1beta1 decodeまたはpatch期待値不一致でFAIL。

- [x] **Step 3: Adapterをv1beta1とdomain分類へ接続する**

Hookはdecode失敗だけ`Failure`を返す。非OSOnly差分は`Success`かつpatchなしとしてCAPIの通常置換へfallbackさせ、OSOnly差分は`spec.image.ref`と`spec.updatePolicy`だけをJSON Merge Patchで返す。

```go
classification, err := inplaceupdate.ClassifyRequest(current, desired)
if err != nil {
    failResponse(resp, err)
    return
}
if !classification.CanUpdateInPlace() {
    succeedWithoutPatch(resp, classification.Rejected)
    return
}
resp.InfrastructureMachinePatch = buildInfraMachinePatch(desired)
```

- [x] **Step 4: extension testを通してコミットする**

Run: `MISE_OFFLINE=1 mise exec -- go test ./infrastructure/http_server/extension -v`

```bash
git add infrastructure/http_server/extension
git commit --signoff -m "feat: OSOnly更新をRuntime Hookで受理する"
```

### Task 3: UpdateMachineから冪等なUpdate Operationを開始する

**Files:**
- Create: `domain/provisioning/workflow/update_machine/orchestrator.go`
- Create: `domain/provisioning/workflow/update_machine/orchestrator_test.go`
- Create: `infrastructure/repository/k8s/inplaceupdate/service.go`
- Create: `infrastructure/repository/k8s/inplaceupdate/service_test.go`
- Modify: `infrastructure/http_server/extension/updatemachine.go`
- Modify: `infrastructure/http_server/extension/server.go`
- Modify: `cmd/controller-manager/main.go`

- [x] **Step 1: 同じrequestを100回処理してOperationが1つになる失敗テストを書く**

desired objects digestから決定的Operation IDを作り、同じMachine UIDとdigestなら同じOperationを返す。異なるdigestのactive Operationは`ErrActivePlanConflict`を返す。

```go
for range 100 {
    go func() {
        _, err := service.Start(ctx, input)
        results <- err
    }()
}
require.Len(t, list.Items, 1)
```

- [x] **Step 2: 未実装でFAILすることを確認する**

Run: `MISE_OFFLINE=1 mise exec -- go test ./domain/provisioning/workflow/update_machine ./infrastructure/repository/k8s/inplaceupdate -v`

Expected: 未定義symbolでFAIL。

- [x] **Step 3: digestとOperation入力を生成する**

```go
type StartInput struct {
    Machine *clusterv1.Machine
    TartMachine *infrastructurev1beta1.TartMachine
    BootstrapConfig runtime.RawExtension
    Host *infrastructurev1beta1.TartHost
}

type Starter interface {
    Start(context.Context, *infrastructurev1beta1.TartHostOperation) (*infrastructurev1beta1.TartHostOperation, error)
}
```

Machine version、Bootstrap spec、Platform Profile等を含むdesired objects digestをCanonical JSONから算出し、Operation IDをMachine UIDとdigestから決定的に生成する。terminal Failedかつ同じdigestのOperationは再作成せず、前回失敗を返す。

- [x] **Step 4: UpdateMachine responseへOperation phaseを写像する**

`Pending`から`RollingBack`までは`Success + RetryAfterSeconds=10`、`Succeeded`は`Success + 0`、`Failed`と`RecoveryRequired`は`Failure`にする。Handlerへclient依存を注入し、global stateを持たない。

- [x] **Step 5: feature gate別登録を追加する**

`InPlaceUpdates`を親gateとし、`InPlaceUpdatesWorker`、`InPlaceUpdatesMultiControlPlane`、`InPlaceUpdatesSingleControlPlane`を段階的にparseする。親gate無効時はRuntime Extension managerを生成せず、無効な対象Machine種別はpatchなしで通常置換へfallbackさせる。

- [x] **Step 6: targeted testを通してコミットする**

Run: `MISE_OFFLINE=1 mise exec -- go test ./domain/provisioning/workflow/update_machine ./infrastructure/repository/k8s/inplaceupdate ./infrastructure/http_server/extension ./cmd -v`

```bash
git add domain/provisioning/workflow/update_machine infrastructure/repository/k8s/inplaceupdate infrastructure/http_server/extension cmd/controller-manager/main.go
git commit --signoff -m "feat: Update Operationを冪等に開始する"
```

### Task 4: Inactive Slot限定の署名済みUpdate Planを生成する

**Files:**
- Create: `domain/provisioning/workflow/update_machine/plan.go`
- Create: `domain/provisioning/workflow/update_machine/plan_test.go`
- Modify: `infrastructure/repository/k8s/agentapi/plan_writer.go`
- Modify: `cmd/kessoku/kessoku.go`
- Modify: `cmd/kessoku/kessoku_band.go`

- [x] **Step 1: slot選択と危険target拒否の失敗テストを書く**

Active Aなら`OS-B`,`Verity-B`だけ、Active Bなら`OS-A`,`Verity-A`だけを許可する。State、Data、active OS/Verity、非OSOnly UpdateClass、manifest generation不一致を拒否する。

- [x] **Step 2: 未実装でFAILすることを確認する**

Run: `MISE_OFFLINE=1 mise exec -- go test ./domain/provisioning/workflow/update_machine -run 'TestBuildUpdatePlan' -v`

Expected: `BuildUpdatePlan`未定義でFAIL。

- [x] **Step 3: Update Plan builderを実装する**

```go
func BuildUpdatePlan(input UpdatePlanInput, keyID string, privateKey ed25519.PrivateKey) (SignedUpdatePlan, error)
```

Planは`OperationTypeUpdate`、現在の`ActiveSlot`、inactive OS/Verity roles、`WriteImage`、`VerifyImage`を持ち、Bootstrap targetを持たない。既存PlanWriterでimmutable Secretへ保存する。

- [x] **Step 4: targeted testを通してコミットする**

Run: `MISE_OFFLINE=1 mise exec -- go test ./domain/provisioning/workflow/update_machine ./infrastructure/repository/k8s/agentapi -v`

```bash
git add domain/provisioning/workflow/update_machine infrastructure/repository/k8s/agentapi cmd/kessoku
git commit --signoff -m "feat: Inactive Slot向け更新Planを生成する"
```

### Task 5: Boot trial、Health Gate、Rollbackを状態機械へ接続する

**Files:**
- Create: `domain/provisioning/entity/inplaceupdate/state.go`
- Create: `domain/provisioning/entity/inplaceupdate/state_test.go`
- Modify: `infrastructure/k8s_controller/tarthostoperation_controller.go`
- Modify: `infrastructure/k8s_controller/tarthostoperation_controller_test.go`
- Modify: `infrastructure/repository/k8s/bootreport/service.go`
- Modify: `infrastructure/repository/k8s/bootreport/service_test.go`
- Modify: `infrastructure/repository/k8s/v1beta1host/service.go`
- Modify: `infrastructure/k8s_controller/tartmachine_v1beta1_controller.go`

- [x] **Step 1: 5種類の失敗とboot試行上限の状態遷移テストを書く**

write、verify、boot、mount、Node health失敗は`RollingBack`へ進む。target boot失敗は最大3回で、4回目は旧slotを選ぶ。旧slot Health Gate成功はOperation Failed、Host Provisioned、Machine Ready trueへ収束し、更新失敗Conditionを残す。旧slotも失敗すればRecoveryRequiredへ進む。

- [x] **Step 2: state reducer未実装でFAILすることを確認する**

Run: `MISE_OFFLINE=1 mise exec -- go test ./domain/provisioning/entity/inplaceupdate -run 'TestTransition' -v`

Expected: 未定義symbolでFAIL。

- [x] **Step 3: 純粋な状態reducerを実装する**

```go
func Transition(state State, event Event) (Decision, error)

type Decision struct {
    Phase operation.Phase
    BootSlot slot.Slot
    Attempt int32
    HostPhase host.Phase
    MachineReady bool
    FailureRetained bool
}
```

不正なphase飛び越し、target slotの再選択、terminal phaseの変更を拒否する。

- [x] **Step 4: controllerとboot reportをreducerへ接続する**

全status更新は`Patch()`で競合再試行し、phase、attempt、last boot report、Conditionsを永続化する。失敗PhaseとslotをEvent、構造化log、Trace attributeへ出す。

- [x] **Step 5: targeted testを通してコミットする**

Run: `MISE_OFFLINE=1 mise exec -- go test ./domain/provisioning/entity/inplaceupdate ./infrastructure/k8s_controller ./infrastructure/repository/k8s/bootreport ./infrastructure/repository/k8s/v1beta1host -v`

```bash
git add domain/provisioning/entity/inplaceupdate infrastructure/k8s_controller infrastructure/repository/k8s
git commit --signoff -m "feat: A/B更新のRollback状態遷移を実装する"
```

### Task 6: 生成物、文書、全体検証を更新する

**Files:**
- Modify: `config/crd/bases/infrastructure.cluster.x-k8s.io_tarthostoperations.yaml`
- Modify: `docs/redesign/tasks/08-ab-update.md`

- [x] **Step 1: APIを変更した場合だけ生成物を更新する**

Run: `MISE_OFFLINE=1 mise run generate`

Expected: controller-genがCRDとDeepCopyを更新する。

- [x] **Step 2: 実装済み条件と未検証条件をTask文書へ記録する**

Task 01未完了のため、dm-verity、bootloader trial、電源断、Node identity維持、E2Eを未検証として残す。単体テストで確認した条件だけを完了証跡へ記録する。

- [x] **Step 3: targeted verificationを実行する**

Run: `MISE_OFFLINE=1 mise exec -- go test ./domain/provisioning/entity/inplaceupdate ./domain/provisioning/workflow/update_machine ./infrastructure/repository/k8s/inplaceupdate ./infrastructure/http_server/extension ./infrastructure/k8s_controller/... -v`

Expected: PASS。

- [x] **Step 4: buildとlintを実行する**

Run: `MISE_OFFLINE=1 mise run build`

Expected: PASS。

Run: `MISE_OFFLINE=1 mise run lint`

Expected: 新規変更に起因するerrorなし。既存違反がある場合はPRへ件数と内容を記録する。

- [ ] **Step 5: 文書と生成物をコミットする**

```bash
git add config api docs/redesign/tasks/08-ab-update.md
git commit --signoff -m "docs: Task 08の実装状況を記録する"
```

- [ ] **Step 6: PRを作成する**

`mise run test-e2e`と`mise run test-provisioning-e2e`はローカル実行せず、PR本文へGitHub Actionsだけで実行すること、Task 01前提が未検証であることを記載する。
