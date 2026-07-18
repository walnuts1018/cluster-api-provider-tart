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

- `domain/<context>/entity`は副作用を持たない値、Smart Constructor、状態遷移、互換性判定、Plan/Decision生成だけを持つ。
- `domain/<context>/workflow`はUse Case単位のI/O Sandwichを実装し、`deps`から必要な情報を読む、Entityへ渡す、結果を副作用Stepで保存する。
- `infrastructure`はKubernetes client、Driver、Artifact、HTTP delivery、Node Lifecycle Engineなどの外部I/Oを実装し、DTO/API型とDomain型の変換境界になる。
- `infrastructure/k8s_controller`はKubernetes objectの取得、pause/deletion、owner確認、patch、Condition/Event出力、Requeue判断だけを担当する。

新規コードでは、controller関数へHost選択、Operation phase遷移、更新可否、retry方針、token判定を直接書いてはならない。

Go向けDMMF指針のうち、「操作名ごとにCommand/Event/Workflowを置く」「状態ごとに型を分ける」「Smart Constructorを使う」「純粋関数をWorkflowから切り出す」という方針は採用する。一方で、「WorkflowがEventHandlerを呼ぶ」「標準`error`を業務失敗の主表現にする」「Web/API/DB DTOを中心に境界を決める」という方針は採用しない。

### 2. 現行実装よりDMMFの型モデルを優先する

現行実装の構造は移行元であり、設計上の制約ではない。次のような箇所は、大きな差分になってもDMMFに寄せて作り直す。

- 業務判断がcontroller、handler、step、adapterへ分散している箇所。
- `error`、文字列、bool、nil pointerの組み合わせで業務状態を表している箇所。
- Workflowが具体的な副作用実行順に強く結合し、純粋なDecision/Planとして読めない箇所。
- mockを順番に呼ぶだけのテストを成立させるためにinterfaceが増えている箇所。
- Kubernetes API型をDomain代わりに使い、invalid stateを型で排除できていない箇所。

移行時は「現行packageを少し整える」ことを目的にしない。まず対象Use Caseのユビキタス言語、入力、状態、失敗、出力Eventを型として定義し、その型に合わせてWorkflow、Step、Infrastructureを組み直す。

### 3. DomainはKubernetes API型へ依存しない

Entity型は原則として `api/v1beta1`、controller-runtime、client-go、JSON/YAML tag、Kubernetes Condition型をimportしない。Kubernetes ResourceからEntity型を作る処理はworkflow model、step、またはinfrastructureへ置く。

例外は、移行途中の既存コードを薄く包む場合に限る。この例外は一時的なものとし、純粋判定を追加・変更する時はDomain型へ切り出す。

### 4. 状態と選択肢はsealed interfaceまたは小さな列挙型で表す

次のような値は、文字列やboolの組み合わせではなく、sealed interfaceまたは明示的な列挙型で表す。

- OperationのCommand、Event、DeadlineOutcome
- Host/Machine/Operationの状態遷移結果
- Node Lifecycle EngineのStep、Health Gate、Preflight結果
- Driver CapabilityとCapability不足の理由
- 期待される業務失敗

sealed interfaceを使う場合は、interfaceに未exported methodを置き、同じpackage内のvariantだけが実装できるようにする。variantを処理するswitchには、全variantを列挙する。新しいvariantを追加した時に処理漏れを検出できるよう、`go-sumtype`または同等の網羅性検査をmise taskまたはlintへ追加する。

単なる状態名だけで付随データを持たない場合は、`type Phase string`のような小さな列挙型を使ってよい。ただし、未知値を外部入力から受け取る境界では`ParseXxx`またはSmart Constructorを通してからDomainへ渡す。

### 5. 期待される業務失敗を標準`error`だけで表さない

Entity/Workflowが呼び出し元に分岐を要求する失敗は、標準`error`だけで返してはならない。次のいずれかで表す。

- `domain/shared/result.Result[T, F]`として返す。
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

Portは「Repository」や「Service」の大きなinterfaceへまとめない。Use Caseが必要とする外部I/Oだけを、Capability別・読み書き別にWorkflow package内で小さく定義する。

既存のDriver方針と同じく、実装できない操作を恒常エラーで返すinterfaceを作らない。Capability不足は、Port呼び出し前のDomain/Application判断で型付き失敗にする。

portの戻り値では、外部システムから来た未信頼値をそのままEntityへ渡さない。InfrastructureまたはWorkflow境界でparseし、Entityへはparse済み型を渡す。

### 8. I/O SandwichをReconcile単位で徹底する

Workflowは次の順序を基本にする。

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

### 10. Use Case単位の縦の境界を維持する

DMMF移行後も、変更はHost allocation、initial provisioning、operation execution、in-place update、secure deliveryのようなUse Case単位で縦に完結させ、次を同じ変更に含める。

1. Domain入力、状態、失敗、Decision/Eventの型定義。
2. API型または外部I/OからDomain型へのparse。
3. 純粋WorkflowまたはDecision関数。
4. WorkflowによるI/O Sandwich。
5. Condition/Event/Requeueへの写像。
6. 純粋関数と型付き失敗の必要十分なテスト。

この移行では、古いWorkflow/Handler/Step構造を温存するためのadapter層を増やさない。古い構造が読みやすさを妨げる場合は削除する。

### 11. 標準パッケージ構成

新規Use Caseまたは大きく改修するUse Caseは、次の構成を標準形とする。全Contextへ未使用directoryを作らず、必要な境界だけを追加する。

```text
api/
  v1beta1/
    ...                         # CRD型。Kubernetes API表現だけを持つ

domain/
  <context>/
    entity/                     # 純粋Entity、Value Object、状態、Failure
    workflow/
      <verb_noun>/              # 1 Workflow = 1 package
        workflow.go             # Workflow struct、NewWorkflow、Do
        ports.go                # Workflowが要求するinterface
        outcome.go              # 必要ならCommand/Event ADTを分割
    step/                       # DIせず直接呼ぶ準純粋関数
    event/                      # 複数Workflowで共有するDomain Event
  shared/
    <concept>/                  # 複数Contextで共有するDomain型
    option/                     # Option[T]
    result/                     # Result[T, F]

dto/                            # JSON protocolなどのAnti-Corruption Layer
infrastructure/
  repository/k8s/<capability>/  # Workflow interfaceのKubernetes実装
  service/driver/<driver>/      # Power/Boot/Media等のinterface実装
  http_server/                  # Agent API、boot、Runtime Extension
  k8s_controller/              # Reconcile entrypoint
  k8s_webhook/                  # Admission boundary
  provisioning_agent/          # Host側の実行境界
utils/                          # Domain語彙を持たない汎用処理
test/                           # architecture、fixture、E2E、横断契約
```

`<context>`はCluster API Providerの業務境界を表し、少なくとも1つのWorkflowを所有する。型を置くだけのcontextは作らず、複数Contextで使う型は`domain/shared/<concept>`へ置く。Workflow directoryは`provision_machine`、`complete_provisioning`、`register_agent`のような動詞と対象のsnake_caseで意図を表す。

### 12. Entityパッケージの規約

`domain/<context>/entity`は、外部世界を知らない純粋な型と関数だけで構成する。標準ファイルは次の意味で使う。

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

外部入力の不正値は、Workflow StepまたはInfrastructureで`ParseXxx`を呼んでEntity型に変換してから渡す。Entity package内では、parse済み型を受け取る関数を優先し、防御的な再validationを散らさない。

### 13. Workflowパッケージの規約

`domain/<context>/workflow/<verb_noun>`は、1つのUse Caseに必要なI/O Sandwichを表す。1 Workflowを1 packageへ閉じ、Entityの純粋Decisionを呼び、package内で定義したinterfaceを使って外部状態を読み書きし、Controllerが扱える結果へ写像する。`domain/<context>/workflow`直下にはGoファイルを置かない。

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
func (workflow *Workflow) Do(
    ctx context.Context,
    command Command,
) result.Result[Event, sharedworkflow.Failure]
```

`Ports`はWorkflowごとのstructにまとめてよいが、interfaceは利用側である同じWorkflow packageに定義する。Context共通の`deps`や`port` packageは作らない。1つのinterfaceに読み取り、書き込み、Event出力、Driver呼び出しを混ぜない。

Workflow packageに置く型は次の通りである。

- `Command`: controllerから渡す最小入力。Kubernetes objectを含めてよいが、Domainへ直接渡さない。
- `Event`: Workflowが完了した事実を過去形のvariantで表す閉じた出力。
- Capability interface: Infrastructureが実装する外部I/O境界。
- APIからDomainへのmapping関数。
- Domain Result/Failure/EventからCondition reason、Kubernetes Event、Requeueへのpresentation関数。

Workflow packageで禁止するものは次の通りである。

- Host選択、phase遷移、token認証、更新可否などの業務判断を直接実装すること。
- EventHandlerをfieldに持ち、Workflow完了後に別Workflowを直接呼ぶこと。
- mock呼び出し順序をテストするためだけのinterfaceを作ること。
- `Do`以外の公開methodを`Workflow`へ追加すること。
- 異なるCommandや結果を持つ複数処理を同じWorkflow packageへ置くこと。
- Stepをinterfaceや`Executor` structとしてDIし、外部clientをStepへ保持させること。
- Workflowの処理を汎用Handler objectへ委譲し、`Do`を空洞化させること。

複数Use Caseの連鎖が必要な場合は、Workflowの外側にProcess Managerを置く。Process ManagerはDomain EventまたはApplication Resultを入力にして、次に呼ぶWorkflowを決める。

### 14. InfrastructureとControllerの規約

Infrastructureはdepsの実装であり、外部I/Oの詳細を閉じ込める。Kubernetes API型、Status patch、SSA、conflict retry、Driver protocol、OCI client、HTTP request/response、Secret参照はInfrastructureに置く。

InfrastructureはDomainの業務判断を再実装してはならない。Infrastructureが行う分岐は、外部APIの表現差、not found/conflictの扱い、retry可能なI/O失敗の分類に限定する。外部I/Oから得た値は、Workflow StepまたはEntityのParse関数を通してから返す。

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
// domain/provisioning/entity/hostallocation/command.go
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
// domain/provisioning/entity/hostallocation/result.go
package hostallocation

import sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"

type Result = sharedresult.Result[Allocated, Failure]

type Allocated struct {
    Host HostRef
    Events []Event
}

```

```go
// domain/provisioning/entity/hostallocation/failure.go
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
// domain/provisioning/entity/hostallocation/decision.go
package hostallocation

import sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"

func Decide(command Command) Result {
    for _, candidate := range command.Candidates {
        match := Match(command.Requirements, candidate)
        switch m := match.(type) {
        case MatchAccepted:
            return sharedresult.Success[Allocated, Failure](Allocated{
                Host: m.Host,
                Events: []Event{
                    HostSelected{Host: m.Host, Machine: command.Machine},
                },
            })
        case MatchRejected:
            continue
        }
    }
    return sharedresult.Failure[Allocated, Failure](
        NoMatchingHost{Requirements: command.Requirements},
    )
}
```

Workflow packageは、外部状態を読み、Domain入力を作り、Domain結果を副作用へ写像する。

```go
// domain/provisioning/workflow/allocate_host/ports.go
package hostallocation

import (
    "context"

    domain "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/entity/hostallocation"
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
// domain/provisioning/workflow/allocate_host/workflow.go
package hostallocation

import (
    "context"
    "fmt"

    domain "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/entity/hostallocation"
)

type Workflow struct {
    ports Ports
}

type ReconcileResult interface {
    isReconcileResult()
}

type HostAllocated struct {
    Host domain.HostRef
}

type AllocationPending struct {
    Failure AllocationFailurePresentation
}

func (HostAllocated) isReconcileResult() {}
func (AllocationPending) isReconcileResult() {}

func NewWorkflow(ports Ports) *Workflow {
    return &Workflow{ports: ports}
}

func (workflow *Workflow) Do(
    ctx context.Context,
    input ReconcileInput,
) (ReconcileResult, error) {
    command, err := CommandFromAPI(input.Machine)
    if err != nil {
        return nil, fmt.Errorf("parse host allocation input: %w", err)
    }

    candidates, err := workflow.ports.Hosts.ListCandidates(ctx, command.Requirements)
    if err != nil {
        return nil, fmt.Errorf("list host candidates: %w", err)
    }
    command.Candidates = candidates

    switch result := domain.Decide(command).(type) {
    case domain.Allocated:
        if err := workflow.ports.Reservations.Reserve(ctx, result.Host, command.Machine); err != nil {
            return nil, fmt.Errorf("reserve host: %w", err)
        }
        if err := workflow.ports.Machines.MarkAllocated(ctx, command.Machine, result.Host); err != nil {
            return nil, fmt.Errorf("mark machine allocated: %w", err)
        }
        return HostAllocated{Host: result.Host}, nil

    case domain.NotAllocated:
        presentation := PresentFailure(result.Failure)
        if err := workflow.ports.Machines.MarkAllocationFailed(ctx, command.Machine, presentation); err != nil {
            return nil, fmt.Errorf("mark machine allocation failed: %w", err)
        }
        return AllocationPending{Failure: presentation}, nil
    }

    panic("unreachable hostallocation.Result")
}
```

ControllerはWorkflowを呼ぶだけにする。

```go
// infrastructure/k8s_controller/tartmachine_controller.go
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
- [DMMF のディレクトリ構成](../../dmmf/new_directory.md)
- [ADR 0005: 能力別Go interfaceを先に実装し、外部ABIはgRPCを候補とする](0005-capability-drivers.md)
