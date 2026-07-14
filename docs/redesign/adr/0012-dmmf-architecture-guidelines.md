# ADR 0012: DMMFをKubernetes Reconcile向けの型安全な境界設計として適用する

- Status: Accepted
- Date: 2026-07-15

## Context

このリポジトリは、GraphQL API ServerではなくCluster API Infrastructure Providerである。主要な入口はHTTP resolverではなくKubernetes Reconciler、Runtime Extension、組み込みnetwork server、Provisioning Agent APIであり、長時間OperationをKubernetes Resourceへ保存しながら再実行する。

Go向けDMMF実装指針を、本Providerでも主要な出発点にする。ただし、そのまま移植するのではなく、Cluster API Provider、Kubernetes Reconcile、長時間Operation、物理host副作用に合わせて作り替える必要がある。

Go向けDMMF実装指針では、本Providerで求める型安全性に対して次の問題が残る。

- WorkflowがEventHandlerを呼び、EventHandlerが別Workflowを呼ぶ構造では、WorkflowとEventHandlerが互いに依存し、依存注入のcomposition rootで循環参照が発生する。
- 期待される業務失敗を標準の`error` interfaceで表すと、呼び出し元が全ケースを処理したかを型やlinterで確認できない。
- API/DB DTOを中心にした説明が多く、Kubernetes Resource、Status、Condition、Event、再実行可能なOperationを正本にする本Providerの境界と一致しない。

現在の実装は参考資料として読むが、踏襲対象ではない。現行のpackage分割、Workflow/Handler/Step構造、`error`の使い方、API型への依存、mock前提のテストがDMMFの妨げになる場合は、互換性維持や差分最小化よりも、型で表現されたドメイン、循環しない依存、読みやすいWorkflowを優先して大幅に変更する。

## Decision

### 1. DMMFガイドを本Provider向けに再解釈する

DMMFは「Web APIの層構造」ではなく、「Reconcileで観測した外部状態を、型付きの判断と明示的な副作用へ分ける設計」として適用する。

- `domain`は副作用を持たない値、Smart Constructor、状態遷移、互換性判定、Plan/Decision生成だけを持つ。
- `application`はUse Case単位のI/O Sandwichを実装し、Portから必要な情報を読む、domainへ渡す、結果をPortで保存する。
- `adapter`はKubernetes client、Driver、Artifact、HTTP delivery、Distribution Lifecycleなどの外部I/Oを実装し、DTO/API型とdomain/application型の変換境界になる。
- `controller`はKubernetes objectの取得、pause/deletion、owner確認、patch、Condition/Event出力、Requeue判断だけを担当する。

新規コードでは、controller関数へHost選択、Operation phase遷移、更新可否、retry方針、token判定を直接書いてはならない。

Go向けDMMF指針のうち、「操作名ごとにCommand/Event/Workflowを置く」「状態ごとに型を分ける」「Smart Constructorを使う」「純粋関数をWorkflowから切り出す」という方針は採用する。一方で、「WorkflowがEventHandlerを呼ぶ」「標準`error`を業務失敗の主表現にする」「Web/API/DB DTOを中心に境界を決める」という方針は採用しない。

### 2. 現行実装よりDMMFの型モデルを優先する

現行実装の構造は移行元であり、設計上の制約ではない。次のような箇所は、大きな差分になってもDMMFに寄せて作り直す。

- 業務判断がcontroller、handler、step、adapterへ分散している箇所。
- `error`、文字列、bool、nil pointerの組み合わせで業務状態を表している箇所。
- Workflowが具体的な副作用実行順に強く結合し、純粋なDecision/Planとして読めない箇所。
- mockを順番に呼ぶだけのテストを成立させるためにinterfaceが増えている箇所。
- Kubernetes API型をDomain代わりに使い、invalid stateを型で排除できていない箇所。

移行時は「現行packageを少し整える」ことを目的にしない。まず対象Use Caseのユビキタス言語、入力、状態、失敗、出力Eventを型として定義し、その型に合わせてapplicationとadapterを組み直す。

### 3. DomainはKubernetes API型へ依存しない

Domain型は原則として `api/v1beta1`、controller-runtime、client-go、JSON/YAML tag、Kubernetes Condition型をimportしない。Kubernetes ResourceからDomain型を作る処理はapplication stepまたはadapterへ置く。

例外は、移行途中の既存コードを薄く包む場合に限る。この例外は一時的なものとし、純粋判定を追加・変更する時はDomain型へ切り出す。

### 4. 状態と選択肢はsealed interfaceまたは小さな列挙型で表す

次のような値は、文字列やboolの組み合わせではなく、sealed interfaceまたは明示的な列挙型で表す。

- OperationのCommand、Event、DeadlineOutcome
- Host/Machine/Operationの状態遷移結果
- Distribution LifecycleのStep、Health Gate、Preflight結果
- Driver CapabilityとCapability不足の理由
- 期待される業務失敗

sealed interfaceを使う場合は、interfaceに未exported methodを置き、同じpackage内のvariantだけが実装できるようにする。variantを処理するswitchには、全variantを列挙する。新しいvariantを追加した時に処理漏れを検出できるよう、`go-sumtype`または同等の網羅性検査をmise taskまたはlintへ追加する。

単なる状態名だけで付随データを持たない場合は、`type Phase string`のような小さな列挙型を使ってよい。ただし、未知値を外部入力から受け取る境界では`ParseXxx`またはSmart Constructorを通してからDomainへ渡す。

### 5. 期待される業務失敗を標準`error`だけで表さない

Domain/Applicationが呼び出し元に分岐を要求する失敗は、標準`error`だけで返してはならない。次のいずれかで表す。

- `Result` sealed interfaceのvariantとして返す。
- `Failure` sealed interfaceを返す。
- Commandに失敗理由を含むDecision/Eventを返す。

標準`error`は、外部I/O失敗、parse不能な未知値、プログラム上の不整合、wrapped errorとして運用者に原因を伝える必要がある失敗に使う。たとえばKubernetes APIの一時失敗、Redfish通信失敗、OCI registry失敗、canonical JSON生成失敗は`error`でよい。

`NoAvailableHost`、`UnsupportedCapability`、`InvalidTransition`、`BootstrapNotReady`、`HealthGateNotSatisfied`のように、呼び出し元がCondition reason、Event、Requeue方針へ写像すべき失敗は型付き失敗へ移す。既存のsentinel errorは、触るUse Caseから順にResult/Failure variantへ置き換える。

### 6. WorkflowはEventHandlerを直接呼ばない

WorkflowからEventHandlerを呼ぶ構造は禁止する。Workflowは主処理の結果として、次のいずれかを返す。

- `Decision`
- `Result`
- `[]Event`
- 次に実行すべき`Command`

後続処理はcomposition root側、controller側、またはApplicationのProcess Managerが結果を解釈して呼び出す。これにより、Workflow A -> EventHandler -> Workflow B -> EventHandlerという再帰的な依存を作らず、依存グラフを一方向に保つ。

Process Managerを置く場合も、複数Workflowを保持してよいのはapplication packageの外側寄りの調停役だけとする。DomainはProcess Managerを知らない。

### 7. PortはUse Caseが必要とする最小Capabilityで分ける

Portは「Repository」や「Service」の大きなinterfaceへまとめない。Use Caseが必要とする操作だけを、Capability別・読み書き別・Step別に小さく定義する。

既存のDriver方針と同じく、実装できない操作を恒常エラーで返すinterfaceを作らない。Capability不足は、Port呼び出し前のDomain/Application判断で型付き失敗にする。

Portの戻り値では、外部システムから来た未信頼値をそのままDomainへ渡さない。AdapterまたはApplication stepでparseし、Domainへはparse済み型を渡す。

### 8. I/O SandwichをReconcile単位で徹底する

Application Workflowは次の順序を基本にする。

1. Kubernetes Resourceや外部Driverから入力を読む。
2. DTO/API型をDomain入力へparseする。
3. Domainの純粋関数でDecision/Plan/Resultを作る。
4. DecisionをPort呼び出し、Status patch、Condition/Event、Requeueへ写像する。

Domain関数はPort、client、clock、logger、recorderを受け取らない。時刻が必要な判定では、Applicationが`now`を値として渡す。

### 9. 型安全性を優先するが、読みやすさを損なう抽象化は避ける

このプロジェクトでは、標準のGoから少し外れてもDMMFの型安全性を優先する。ただし、型を増やす目的は読み手が状態と責務を追いやすくすることであり、抽象化そのものを目的にしない。

- primitive obsessionを避けるため、Operation ID、Plan Digest、Session Token Digest、Artifact Digest、Slot、Capabilityは専用型を使う。
- optionalな複合状態は、nil pointerよりもsealed interfaceやResult variantで表す。
- sliceやmapをDomain型に保持する場合は、constructorまたはgetterでcopyし、呼び出し元の変更がDomain不変条件を壊さないようにする。
- 既存のCRD API型はKubernetes互換性のためにexported fieldを持つが、Domain型まで同じ形にしない。

### 10. 移行はUse Case単位で縦に切る

DMMF移行は横断的な一括リネームではなく、Use Case単位で行う。たとえばHost allocation、initial provisioning、operation execution、in-place update、secure deliveryのような単位で、次を同じ変更に含める。

1. Domain入力、状態、失敗、Decision/Eventの型定義。
2. API型または外部I/OからDomain型へのparse。
3. 純粋WorkflowまたはDecision関数。
4. ApplicationによるI/O Sandwich。
5. Condition/Event/Requeueへの写像。
6. 純粋関数と型付き失敗の必要十分なテスト。

この移行では、古いWorkflow/Handler/Step構造を温存するためのadapter層を増やさない。古い構造が読みやすさを妨げる場合は削除する。

### 11. 標準パッケージ構成

新規Use Caseまたは大きく改修するUse Caseは、次の構成を標準形とする。既存の`handler`、`step`、`model`分割は移行元として扱い、新しい標準形として増やさない。

```text
api/
  v1beta1/
    ...                         # CRD型。Kubernetes API表現だけを持つ

internal/
  domain/
    <usecase>/
      command.go                # 純粋Workflowへの入力。外部I/O型を含めない
      event.go                  # Workflowが発生させたDomain Event
      failure.go                # 呼び出し元が分岐すべき業務失敗のsealed interface
      result.go                 # 成功/失敗/保留などのWorkflow戻り値
      workflow.go               # 副作用なしのWorkflow本体
      <state>.go                # 状態型、値オブジェクト、Smart Constructor
      <decision>.go             # 長くなる純粋判定。例: select_host.go
      *_test.go                 # 純粋関数、状態遷移、型付き失敗のテスト
    shared/
      <concept>/                # 複数Use Caseで共有する純粋な概念だけを置く

  application/
    <usecase>/
      workflow.go               # I/O Sandwichを組み立てるApplication Workflow
      ports.go                  # このUse Caseが要求する最小Port
      mapping.go                # API/Adapter入力をDomain入力へparseする
      effects.go                # Domain DecisionをPort呼び出しへ写像する
      presentation.go           # Condition/Event/Requeue/Status reasonへの写像
      process_manager.go        # 複数Use Caseを調停する場合だけ置く
      *_test.go                 # Port境界とpresentation写像の代表ケース

  adapter/
    k8s/
      <capability>/
        service.go              # application PortのKubernetes実装
        mapping.go              # Kubernetes API型とapplication/domain型の変換
        status_patch.go         # Status/Condition patchの実装
        event.go                # Kubernetes Event出力
        *_test.go               # API変換、patch、conflict/idempotencyのテスト
    driver/
      <driver>/
        service.go              # Power/Boot/Media等のPort実装
        mapping.go              # Driver設定とdomain型の変換
        *_test.go               # Driver contractまたはprotocol境界のテスト

  controller/
    <resource>_controller.go    # Reconcile入口。Application Workflowを呼ぶだけにする
    <resource>_conditions.go    # controller固有のpatch補助が必要な場合だけ置く
```

`<usecase>`はCluster API Providerの業務単位を表す小文字のpackage名にする。例は`hostallocation`、`initialprovisioning`、`operationexecution`、`inplaceupdate`、`securedelivery`、`machinedeletion`、`distributionlifecycle`である。Go package名は原則として小文字の1語にし、snake_case package名を増やさない。略語で意味が落ちる場合は、directory名を長くしてよい。

### 12. Domain Use Caseパッケージの規約

Domain Use Case packageは、外部世界を知らない純粋な型と関数だけで構成する。標準ファイルは次の意味で使う。

| ファイル | 必須 | 内容 |
|---|---|---|
| `command.go` | 必須 | Workflowへの入力型。Kubernetes object、client、contextを含めない |
| `workflow.go` | 必須 | `func Decide(command Command) Result`または`func Execute(command Command) Result`のような純粋関数 |
| `result.go` | 必須 | 成功、保留、失敗、次Commandなどを表すsealed interfaceまたはstruct |
| `failure.go` | 分岐が必要な失敗がある場合は必須 | 業務失敗のsealed interface。標準`error`だけで代用しない |
| `event.go` | Eventがある場合は必須 | Domain Event。Kubernetes Eventではない |
| `<state>.go` | 必要に応じて必須 | 状態型、値オブジェクト、Smart Constructor、Parse関数 |
| `<decision>.go` | 任意 | `workflow.go`から切り出した純粋な判定単位 |

Domain Use Case packageで禁止するものは次の通りである。

- `context.Context`
- `client.Client`
- `record.EventRecorder`
- `api/v1beta1`
- controller-runtime、client-go、HTTP、OCI、Redfish、filesystem、clock、logger
- JSON/YAML tag付きDTO
- mock生成用interface

外部入力の不正値は、ApplicationまたはAdapterで`ParseXxx`を呼んでDomain型に変換してから渡す。Domain package内では、parse済み型を受け取る関数を優先し、防御的な再validationを散らさない。

### 13. Application Use Caseパッケージの規約

Application Use Case packageは、1つのUse Caseに必要なI/O Sandwichを表す。Domainの純粋Workflowを呼び、Portを使って外部状態を読み書きし、Controllerが扱える結果へ写像する。

標準の公開面は次の形にする。

```go
type Workflow struct {
    ports Ports
}

type Ports struct {
    Hosts      HostReader
    Operations OperationWriter
    Recorder   EventRecorder
}

func NewWorkflow(ports Ports) *Workflow
func (workflow *Workflow) Reconcile(ctx context.Context, input ReconcileInput) (ReconcileResult, error)
```

`Ports`はUse Caseごとのstructにまとめてよいが、各fieldの型は小さなinterfaceにする。1つのinterfaceに読み取り、書き込み、Event出力、Driver呼び出しを混ぜない。たとえばHost予約Use Caseでは、`HostSelector`、`HostReservationWriter`、`OperationStarter`を分ける。

Application packageに置く型は次の通りである。

- `ReconcileInput`: controllerから渡す最小入力。Kubernetes objectを含めてよいが、Domainへ直接渡さない。
- `ReconcileResult`: patch済みか、requeueするか、Condition/Eventを出したかを表すApplication結果。
- Port interface: Adapterが実装する外部I/O境界。
- APIからDomainへのmapping関数。
- Domain Result/Failure/EventからCondition reason、Kubernetes Event、Requeueへのpresentation関数。

Application packageで禁止するものは次の通りである。

- Host選択、phase遷移、token認証、更新可否などの業務判断を直接実装すること。
- EventHandlerをfieldに持ち、Workflow完了後に別Workflowを直接呼ぶこと。
- mock呼び出し順序をテストするためだけのinterfaceを作ること。

複数Use Caseの連鎖が必要な場合は、`process_manager.go`を置く。Process ManagerはApplication層の調停役であり、Domain packageには置かない。Process ManagerはDomain EventまたはApplication Resultを入力にして、次に呼ぶApplication Workflowを決める。

### 14. AdapterとControllerの規約

AdapterはPortの実装であり、外部I/Oの詳細を閉じ込める。Kubernetes API型、Status patch、SSA、conflict retry、Driver protocol、OCI client、HTTP request/response、Secret参照はAdapterに置く。

AdapterはDomainの業務判断を再実装してはならない。Adapterが行う分岐は、外部APIの表現差、not found/conflictの扱い、retry可能なI/O失敗の分類に限定する。外部I/Oから得た値は、ApplicationまたはDomainのParse関数を通してから返す。

ControllerはApplication Workflowの呼び出しに徹する。標準形は次の責務だけを持つ。

1. Reconcile対象objectを取得する。
2. pause、deletion、owner確認などCAPI controller共通の入口条件を処理する。
3. Application Workflowへ`ReconcileInput`を渡す。
4. `ReconcileResult`に従ってpatch、Event、requeue結果を返す。

ControllerにUse Case固有の状態遷移やCondition reason選択を書き始めた場合は、ApplicationまたはDomainへ戻す。

### 15. 実装例

以下はHost allocationを例にした最小形である。実際のUse Caseでは名前とvariantを業務語彙に合わせる。

Domain packageは、入力、成功、失敗、Eventを型として表す。

```go
// internal/domain/hostallocation/command.go
package hostallocation

type Command struct {
    Machine MachineRef
    Requirements Requirements
    Candidates []Candidate
}

type MachineRef struct {
    Name string
    UID  string
}
```

```go
// internal/domain/hostallocation/result.go
package hostallocation

type Result interface {
    isResult()
}

type Allocated struct {
    Host HostRef
    Events []Event
}

type NotAllocated struct {
    Failure Failure
}

func (Allocated) isResult() {}
func (NotAllocated) isResult() {}
```

```go
// internal/domain/hostallocation/failure.go
package hostallocation

type Failure interface {
    isFailure()
}

type NoMatchingHost struct {
    Requirements Requirements
}

type HostAlreadyAssigned struct {
    Host HostRef
}

type UnsupportedCapability struct {
    Missing []Capability
}

func (NoMatchingHost) isFailure() {}
func (HostAlreadyAssigned) isFailure() {}
func (UnsupportedCapability) isFailure() {}
```

```go
// internal/domain/hostallocation/workflow.go
package hostallocation

func Decide(command Command) Result {
    for _, candidate := range command.Candidates {
        match := Match(command.Requirements, candidate)
        switch m := match.(type) {
        case MatchAccepted:
            return Allocated{
                Host: m.Host,
                Events: []Event{
                    HostSelected{Host: m.Host, Machine: command.Machine},
                },
            }
        case MatchRejected:
            continue
        }
    }
    return NotAllocated{Failure: NoMatchingHost{Requirements: command.Requirements}}
}
```

Application packageは、外部状態を読み、Domain入力を作り、Domain結果を副作用へ写像する。

```go
// internal/application/hostallocation/ports.go
package hostallocation

import (
    "context"

    domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/hostallocation"
)

type HostCandidateReader interface {
    ListCandidates(context.Context, domain.Requirements) ([]domain.Candidate, error)
}

type HostReservationWriter interface {
    Reserve(context.Context, domain.HostRef, domain.MachineRef) error
}

type MachineStatusWriter interface {
    MarkAllocated(context.Context, domain.MachineRef, domain.HostRef) error
    MarkAllocationFailed(context.Context, domain.MachineRef, AllocationFailurePresentation) error
}

type Ports struct {
    Hosts HostCandidateReader
    Reservations HostReservationWriter
    Machines MachineStatusWriter
}
```

```go
// internal/application/hostallocation/workflow.go
package hostallocation

import (
    "context"
    "fmt"

    domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/hostallocation"
)

type Workflow struct {
    ports Ports
}

func NewWorkflow(ports Ports) *Workflow {
    return &Workflow{ports: ports}
}

func (workflow *Workflow) Reconcile(
    ctx context.Context,
    input ReconcileInput,
) (ReconcileResult, error) {
    command, err := CommandFromAPI(input.Machine)
    if err != nil {
        return ReconcileResult{}, fmt.Errorf("parse host allocation input: %w", err)
    }

    candidates, err := workflow.ports.Hosts.ListCandidates(ctx, command.Requirements)
    if err != nil {
        return ReconcileResult{}, fmt.Errorf("list host candidates: %w", err)
    }
    command.Candidates = candidates

    switch result := domain.Decide(command).(type) {
    case domain.Allocated:
        if err := workflow.ports.Reservations.Reserve(ctx, result.Host, command.Machine); err != nil {
            return ReconcileResult{}, fmt.Errorf("reserve host: %w", err)
        }
        if err := workflow.ports.Machines.MarkAllocated(ctx, command.Machine, result.Host); err != nil {
            return ReconcileResult{}, fmt.Errorf("mark machine allocated: %w", err)
        }
        return ReconcileResult{Requeue: false}, nil

    case domain.NotAllocated:
        presentation := PresentFailure(result.Failure)
        if err := workflow.ports.Machines.MarkAllocationFailed(ctx, command.Machine, presentation); err != nil {
            return ReconcileResult{}, fmt.Errorf("mark machine allocation failed: %w", err)
        }
        return ReconcileResult{RequeueAfter: presentation.RequeueAfter}, nil
    }

    panic("unreachable hostallocation.Result")
}
```

ControllerはApplication Workflowを呼ぶだけにする。

```go
// internal/controller/tartmachine_controller.go
package controller

func (r *TartMachineReconciler) reconcileNormal(
    ctx context.Context,
    machine *infrastructurev1beta1.TartMachine,
) (ctrl.Result, error) {
    result, err := r.hostAllocation.Reconcile(ctx, hostallocation.ReconcileInput{
        Machine: machine,
    })
    if err != nil {
        return ctrl.Result{}, err
    }
    return result.ControllerResult(), nil
}
```

この例で重要なのは、`domain.Decide`がKubernetes client、context、logger、recorderを受け取らないこと、`NoMatchingHost`が`error`ではなく`Failure` variantであること、Controllerが`NoMatchingHost`のCondition reasonを選ばないことである。

## Consequences

- 現行実装と大きく異なるpackage構造やWorkflow構造を許容する。
- WorkflowとEventHandlerの循環参照を作らず、composition rootで依存を一方向に組み立てられる。
- 期待される業務失敗を型付きで扱えるため、Condition reason、Event、Requeue方針への写像漏れを検出しやすくなる。
- Domainの単体テストはKubernetes clientやmockを使わず、入力とDecision/Resultの対応を検証できる。
- ApplicationのテストはPort境界の少数の代表ケースに絞り、mock呼び出し順序だけを検証するテストを避けられる。
- sealed interfaceの網羅性検査を導入するまで、Go compilerだけではswitchの処理漏れを検出できない。Taskでlintまたはmise taskを追加する必要がある。
- 既存コードには標準`error`で業務失敗を返す箇所がある。移行はUse Case単位で行い、触る範囲では古い表現を残さない。
- 差分は大きくなるが、長期的には状態、失敗、後続処理が型とファイル構造から読めるようになる。

## Alternatives

- Web API向けのGo DMMF指針をそのまま採用する: GraphQL/DB中心の構造になり、Reconcile、Operation CR、Controller/Agent境界と合わない。EventHandler経由のWorkflow連鎖で循環依存が起きる。
- 現行実装を基本的に踏襲する: 短期差分は小さいが、DMMFに従っていない構造、型安全でない失敗表現、読みにくいWorkflowを固定化してしまう。
- Go標準の`error`だけを使い続ける: Goらしさは保てるが、呼び出し元が業務失敗を網羅したか確認できず、Condition reasonやEventの欠落が実行時まで分からない。
- Controllerに判断を直接書く: 初期実装は速いが、CAPI contract、Operation再開、Driver capability、Security条件がcontrollerに混ざり、実機なしで検証できる純粋ロジックが失われる。
- すべてを巨大なApplication Serviceに集約する: DIは単純に見えるが、Use CaseごとのPortとResultが曖昧になり、Capability不足や状態遷移の型安全性が落ちる。

## References

- [アーキテクチャ](../architecture.md)
- [AI Agent向けWorkflow設計規約](../ai-workflow-architecture.md)
- [ADR 0005: 能力別Go interfaceを先に実装し、外部ABIはgRPCを候補とする](0005-capability-drivers.md)
