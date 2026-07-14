# AI Agent向けWorkflow設計規約

## 1. 目的

この文書は、再設計作業中にAI AgentがWorkflow、Step、Event、Handler、Modelの境界を一貫して保つための実装規約である。対象は`internal/application`配下のApplication層と、そこから呼び出される`internal/domain`配下の純粋な判断である。

## 2. パッケージの役割

各Application bounded contextは、必要に応じて次の小パッケージへ分割する。

| パッケージ | 役割 | 依存してよいもの |
|---|---|---|
| `model` | Workflow内で使う入力状態、結果、DTOからdomainへ渡す観測状態 | API型、domain型 |
| `event` | Workflowが発生させるApplication event | primitive、domain value、API識別子 |
| `handler` | EventまたはCommandを受け取り、対応するStep実行を選ぶ | `step`、`model`、domain command/event |
| `port` | 外部副作用を実行する境界interface | domain型、API型 |
| `step` | 再利用可能な純粋Step、またはPortを使う副作用Step executor | `port`、domain型、API型 |
| `workflow` | Workflowの公開入口。Process Manager、Handler、Stepを組み立てる | `model`、`handler`、`step`、`port` |

`workflow`パッケージに巨大なprivate関数を追加してはならない。複数Workflowで使える判断はEntity/Value Objectのメソッド、または`step`パッケージの純粋関数へ移す。

## 3. 依存ルール

- Workflowは別のWorkflowを直接フィールドに持ってはならない。
- Workflowを順番に実行する必要がある場合は、controller、composition root、またはevent handlerの呼び出し側でpipelineとして並べる。
- Workflowから別Workflow相当の処理を使う場合は、必要な操作だけを表すStep interfaceを定義して注入する。
- HandlerはCommand/EventをStepへ写像する。Handler自体にKubernetes patchやdriver呼び出しを直接書かない。
- Stepは冪等な最小単位にする。副作用StepはPortを経由し、純粋StepはPortを受け取らない。
- Domain Process Managerは副作用を呼ばず、現在状態とevent/commandから次状態とcommandを返す。

## 4. Workflow実装の形

Workflowの責務は次に限定する。

1. API/adapterから受け取った入力を`model`またはdomain stateへ変換する。
2. domain Process Managerまたは純粋Stepを呼び、Command/Eventを得る。
3. Command/Eventを`handler`へ渡す。
4. 必要なPort/Step/Handlerをconstructorで組み立てる。

WorkflowがKubernetes Resourceのpatch、driver呼び出し、Plan保存、Event記録を直接行っている場合は、`step`または`handler`へ分離する。

## 5. Stepの再利用

Stepは特定Workflow専用に閉じ込めない。次のいずれかに該当する処理は共有Stepへ切り出す。

- API型からdomain型への変換。
- Operation phaseからroute/commandを選ぶ判断。
- Host reference、Operation reference、Plan digestなど複数Workflowで同じ意味を持つ状態解釈。
- Kubernetes patch、driver観測、Plan永続化など、同じPortと同じ冪等性で表せる副作用。

## 6. 移行時の判断基準

- private methodが5個を超えるWorkflowは、まず`step.Executor`または`handler`へ分割する。
- switchがdomain command/eventを分岐している場合は`handler`へ移す。
- switchがdomain/API enumを変換している場合は純粋StepかEntity/Value Objectのメソッドへ移す。
- `*OtherWorkflow`を引数やフィールドに見つけたら、必要なメソッドだけを持つStep interfaceへ置き換える。
- 新しいテストは、Process Managerや純粋Stepの重要分岐に限定する。副作用順序をモックでなぞるだけのテストは追加しない。
