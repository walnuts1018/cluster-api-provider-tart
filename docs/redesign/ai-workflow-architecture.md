# AI Agent向けWorkflow設計規約

## 1. 目的

この文書は、再設計作業中にAI AgentがWorkflow、Step、Event Manager、Entity、Modelの境界を一貫して保つための実装規約である。対象は`domain/<context>`配下のWorkflowと、そこから呼び出される純粋なEntity/Stepである。

## 2. パッケージの役割

`domain/<context>`全体のContext分類は、[Application Workflowパッケージ整理方針](application-workflow-package-map.md)を正本とする。この文書は、各Context内でWorkflow、Step、Event、Event Manager、Modelを実装するときの境界規約を定める。

各Application bounded contextは、必要に応じて次の小パッケージへ分割する。

| パッケージ | 役割 | 依存してよいもの |
|---|---|---|
| `workflow/model` | Workflow内で使う入力状態、結果、DTOからEntityへ渡す観測状態 | API型、Entity型 |
| `events` | Workflowが発生させるApplication event | primitive、Domain value、API識別子 |
| `event_manager` | EventまたはCommandを受け取り、対応するStep実行を選ぶ | `steps`、`workflow/model`、Domain command/event |
| `deps` | 外部副作用を実行する境界interface | Domain型、API型 |
| `steps` | 再利用可能な純粋Step、またはdepsを使う副作用Step executor | `deps`、Entity型、API型 |
| `workflow` | Workflowの公開入口。Process ManagerとStepをI/O Sandwichとして組み立てる | `workflow/model`、`steps`、`deps`、`entity` |
| `entity` | Entity、Value Object、状態遷移、閉じたFailure | 他Contextの`entity`、`domain/shared` |

`workflow`パッケージに巨大なprivate関数を追加してはならない。複数Workflowで使える判断はEntity/Value Objectのメソッド、または`step`パッケージの純粋関数へ移す。

## 3. 依存ルール

- Workflowは別のWorkflowを直接フィールドに持ってはならない。
- Workflowのconstructorは別のWorkflowを生成してはならない。具象Workflowの組み合わせはcontrollerやcomposition rootで行う。
- Workflowを順番に実行する必要がある場合は、controller、composition root、またはevent handlerの呼び出し側でpipelineとして並べる。
- Workflowから別Workflow相当の処理を使う場合は、必要な操作だけを表すStep interfaceを`deps`へ定義して注入する。
- Event ManagerはCommand/EventをStepへ写像する。Event Manager自体にKubernetes patchやdriver呼び出しを直接書かない。
- Stepは冪等な最小単位にする。副作用StepはPortを経由し、純粋StepはPortを受け取らない。
- Domain Process Managerは副作用を呼ばず、現在状態とevent/commandから次状態とcommandを返す。

## 4. Workflow実装の形

Workflowの責務は次に限定する。

1. API/infrastructureから受け取った入力を`workflow/model`またはEntity stateへ変換する。
2. Domain Process Managerまたは純粋Stepを呼び、`Result`、Command、Eventを得る。
3. `Result`の全variantを処理し、必要な副作用Stepへ渡す。
4. 必要なdeps/Stepをcomposition rootで組み立てる。

WorkflowがKubernetes Resourceのpatch、driver呼び出し、Plan保存、Event記録を直接行っている場合は、`steps`または`event_manager`へ分離する。

Workflow結果で排他的な状態をpointerの有無やboolの組み合わせとして表してはならない。状態variantはsealed interface、optional値は`option.Option[T]`、期待される失敗は`result.Result[T, F]`と閉じた`Failure`で表す。

## 5. Stepの再利用

Stepは特定Workflow専用に閉じ込めない。次のいずれかに該当する処理は共有Stepへ切り出す。

- API型からdomain型への変換。
- Operation phaseからroute/commandを選ぶ判断。
- Host reference、Operation reference、Plan digestなど複数Workflowで同じ意味を持つ状態解釈。
- Kubernetes patch、driver観測、Plan永続化など、同じPortと同じ冪等性で表せる副作用。

## 6. 移行時の判断基準

- private methodが5個を超えるWorkflowは、まず`steps.Executor`または`event_manager`へ分割する。
- switchがdomain command/eventを分岐している場合は`event_manager`へ移す。
- switchがdomain/API enumを変換している場合は純粋StepかEntity/Value Objectのメソッドへ移す。
- `*OtherWorkflow`を引数やフィールドに見つけたら、必要なメソッドだけを持つStep interfaceへ置き換える。
- 新しいテストは、Process Managerや純粋Stepの重要分岐に限定する。副作用順序をモックでなぞるだけのテストは追加しない。
