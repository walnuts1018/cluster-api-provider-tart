# Application Workflowパッケージ整理方針

## 1. 目的

この文書は、`internal/application`配下の全Workflowを同じ基準で整理するための横断方針である。個別Contextのリファクタリングは、この分類と依存ルールを先に満たすように進める。

## 2. パッケージ分類

Application層のパッケージは、次のいずれかに分類する。

| 分類 | 役割 | Workflowの責務 | 例 |
|---|---|---|---|
| Composition Workflow | 複数Stepを順序付ける上位のApplication entrypoint | domain decisionを得て、Handlerまたは注入されたStep interfaceへ渡す | `clusterlifecycle`, `machinelifecycle`, `machinetemplatelifecycle` |
| Process Manager Workflow | 長時間Operationの状態とcommandを進める | Process ManagerのdecisionをHandlerへ渡す | `operationexecution`, `machinedeletion`, `machineexecution` |
| Operation Start Workflow | Operation作成とPlan永続化のI/O sandwichを組む | draft生成、署名、開始、Plan保存をStepとして順序付ける | `initialprovisioning`, `inplaceupdate`, `cleaning` |
| Reusable Step Context | 複数Workflowから使う副作用Stepまたは純粋Stepを提供する | Workflowに見える小さいStep interfaceを提供する | `resourcefinalizer`, `clusterstatus`, `distributionlifecycle` |
| Agent-side Workflow | Agent内で署名検証済みcommandを実行する | 外部入力の検証後にStep runnerへ渡す | `nodelifecycle` |
| Status/Model Helper | API StatusやConditionの純粋な組み立てを提供する | Workflowを持たない | `machineallocation`, `machinehealth` |

## 3. 共通パッケージ構成

各Contextは必要な範囲で次の小パッケージへ分ける。全Contextへ機械的に空パッケージを作ってはならない。

```text
internal/application/<context>/
├── workflow.go          # 公開entrypointとcompositionだけ
├── model/               # Workflow入出力、観測結果、effect結果のADT
├── event/               # Workflowが返すApplication event
├── handler/             # decision/command/eventをStepへ写像する
├── step/                # 純粋Step、またはPortを使う副作用Step executor
├── port/                # 外部副作用のinterface
└── *.go                 # Context固有の純粋関数。大きくなったらmodel/stepへ移す
```

`workflow.go`にprivate helperが増えた場合は、次の基準で移動先を決める。

| 処理 | 移動先 |
|---|---|
| API objectから観測状態を作る | `model`または`step` |
| domain decisionを副作用へ写像する | `handler` |
| Kubernetes patch、driver呼び出し、Plan保存 | `step.Executor` |
| 署名、digest、Plan構築などの純粋変換 | `step`またはEntity/Value Object |
| Application eventの定義 | `event` |

## 4. 依存ルール

- Workflowは別Workflowの具象型へ依存してはならない。
- Workflowのfield名や型名に`Workflow`を含む依存を置いてはならない。例外は互換移行中の既存aliasだけで、触るタイミングでStep interface名へ変更する。
- Workflow間の順序実行はcontrollerまたはcomposition rootでpipelineとして組む。
- Event handlerが複数Workflowを呼ぶことは許可する。ただしHandlerは具象Workflowではなく必要なStep interfaceに依存する。
- Reusable Step Contextを呼ぶ場合は、呼び出し元Context側に必要なmethodだけを持つinterfaceを定義する。
- `handler`は分岐とStep呼び出しの対応だけを持つ。Kubernetes patch、driver呼び出し、Plan保存を直接実装しない。
- `step`の副作用実装はPortを経由する。純粋StepはPortを受け取らない。
- `model`はWorkflow内の状態をADTで表す。未使用の汎用structや`any`での持ち回りは禁止する。

## 5. 既存Contextの方針

| Context | 現状 | 次の整理方針 |
|---|---|---|
| `clusterlifecycle` | FinalizerとCluster statusを直接順序付けるComposition Workflow | `handler`を追加し、active/deleting decisionからStep interfaceへ写像する。`clusterstatus.Workflow`具象への依存は禁止を維持する |
| `machinelifecycle` | Finalizer、Execution、DeletionをStep interfaceとして順序付けるがWorkflow内switchが大きい | lifecycle command handlerを追加し、削除結果の分岐を`model`へ寄せる |
| `machinetemplatelifecycle` | Finalizerだけの薄いWorkflow | decision handlerを追加するか、Finalizer専用Workflowとして薄さを維持する。private分岐が増えたらhandlerへ移す |
| `resourcefinalizer` | 汎用Reusable Step Context | 既存構成を維持する。Workflow名は外部互換の入口として残し、呼び出し側はFinalizerStep interfaceへ依存する |
| `clusterstatus` | Handler/Model/Stepへ分離済み | 現状維持。Status組み立ての純粋関数を増やす場合は`step`またはdomainへ置く |
| `machinedeletion` | Handler/Model/Stepへ分離済みだが`CleaningWorkflow`名が残る | `CleaningWorkflow`を`CleaningStep`へ改名し、Workflow依存に見える型名を消す |
| `operationexecution` | Process Manager + Handler + Stepに分離済み | 現状維持。Step executor内のcommand分岐が増えた場合はhandlerへ戻す |
| `machineexecution` | Model/Step分離途中。WorkflowとStepExecutorに副作用分岐が多い | Handlerを追加し、Provision/Update/Healthのdecision適用switchを段階的に移す |
| `initialprovisioning` | Operation開始とHost予約をWorkflowに直書き | `model`/`handler`/`step.Executor`を追加し、Host予約、Operation開始、CompletionをStep interface化する |
| `inplaceupdate` | Operation開始とPlan永続化をWorkflowに直書き | `model`/`handler`/`step.Executor`を追加し、Agent PlanとNode Lifecycle Plan保存をStep化する |
| `cleaning` | Operation開始とPlan永続化をWorkflowに直書き | `initialprovisioning`/`inplaceupdate`と同じOperation Start Workflow形へ揃える |
| `distributionlifecycle` | Step Handlerでdriver dispatch済み | 現状維持。`nodelifecycle`からはStepRunner interface越しに使う |
| `nodelifecycle` | 署名検証後にStepRunnerへ渡すAgent-side Workflow | 現状維持。`distributionlifecycle.Workflow`具象へ依存しない |
| `machineallocation` | Status helper | Workflow化しない |
| `machinehealth` | Status helper | Workflow化しない |

## 6. 実装順序

横断整理は次の順に行う。

1. 上位LifecycleのHandler境界を揃える。対象は`clusterlifecycle`、`machinelifecycle`、`machinetemplatelifecycle`。
2. Workflow依存に見える型名をStep名へ置き換える。対象は`machinedeletion.CleaningWorkflow`、`machineexecution.ProvisionWorkflow`。
3. Operation Start Workflowを共通形へ揃える。対象は`initialprovisioning`、`inplaceupdate`、`cleaning`。
4. Process Manager Workflowに残る副作用switchをHandlerへ移す。対象は`machineexecution`。
5. 既存の`model`/`step`へ移したADTと純粋Stepを、複数Contextで再利用できる粒度へ調整する。

この順序は、個別ドメインの都合より優先する。例外が必要な場合は、変更PRまたはコミットで理由を残す。
