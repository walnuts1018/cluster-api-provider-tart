# Workflowパッケージ整理方針

## 1. 目的

この文書は、`domain/<context>/workflow/<verb_noun>`配下のWorkflowを同じ基準で整理するための横断方針である。正本となる配置規則は`docs/dmmf/new_directory.md`、依存方向はADR 0012に従う。

## 2. Workflowの定義

Workflowは`Command`を受け取り、I/O Sandwichを順序付けて`Result[Event, Failure]`を返す`Workflow` structと`Do` methodの組である。この契約に例外は設けない。

- 1 Workflowを1 packageへ置く。
- `domain/<context>/workflow/`直下へGoファイルを置かない。
- Workflow packageは`Workflow` structをちょうど1つ持つ。
- `Workflow`が公開する実行methodは`Do`だけとする。
- 依存interfaceは利用側である同じWorkflow packageへ置く。
- 別のCommand、入力、結果、冪等性境界を持つ処理は別Workflow packageへ分ける。

## 3. 標準構成

```text
domain/<context>/
├── entity/                       # Context固有のEntity、状態、Failure、Decision
├── workflow/
│   ├── <verb_noun>/
│   │   ├── workflow.go           # Workflow、NewWorkflow、Do
│   │   ├── ports.go              # このWorkflowが要求するinterface
│   │   └── outcome.go            # 必要ならCommand/Event ADTを分割
│   └── <another_verb_noun>/
│       └── ...
├── event/                        # Context内で共有するDomain Event
└── step/                         # Workflowから直接呼ぶ準純粋関数
```

## 4. 現在のWorkflow map

| Context | Workflow package | Command / intent |
|---|---|---|
| `provisioning` | `workflow/allocate_host` | 条件に合うHostを選択・予約する |
| `provisioning` | `workflow/provision_machine` | Provision OperationとPlanを開始する |
| `provisioning` | `workflow/complete_provisioning` | Provision完了をOperationとHostへ反映する |
| `provisioning` | `workflow/update_machine` | Update OperationとPlanを開始する |
| `provisioning` | `workflow/delete_machine` | 削除時のCleaningを収束させる |
| `provisioning` | `workflow/do_cleaning` | Cleaning OperationとPlanを開始する |
| `provisioning` | `workflow/reconcile_machine` | TartMachineの実行状態を収束させる |
| `provisioning` | `workflow/reconcile_machine_lifecycle` | Active/Delete lifecycleを調停する |
| `provisioning` | `workflow/execute_operation` | TartHostOperationを実行する |
| `cluster` | `workflow/reconcile_cluster` | TartCluster lifecycleを収束させる |
| `cluster` | `workflow/reconcile_cluster_status` | CAPI Cluster観測をStatusへ反映する |
| `cluster` | `workflow/reconcile_machine_template` | Template finalizerを収束させる |
| `node` | `workflow/run_signed_step` | 署名検証済みLifecycle Stepを実行する |
| `node` | `workflow/run_step` | runtime別Lifecycle Stepを実行する |
| `agentdelivery` | `workflow/register_agent` | Agent登録とSession発行を行う |
| `agentdelivery` | `workflow/bootstrap_agent` | Bootstrapをsingle-shot配信する |

## 5. Bounded Contextの粒度

`domain/<context>`は、少なくとも1つのWorkflowと、そのWorkflow内で意味が閉じるユビキタス言語を持つ単位とする。Entityの置き場所を作るためだけにContextを分割しない。

`operation`、`driver`、`slot`、`agentsession`、`machinehealth`のように複数ContextやInfrastructure境界から利用される型は、`domain/shared/<concept>`へ置く。共有型にもSmart Constructorとinvariantを持たせ、primitiveや雑多なhelperの退避先にはしない。

## 6. Workflowではない処理

Driver registry、Finalizer操作、Kubernetes Status presentationなど、CommandからEventを作らない処理をWorkflowと呼ばない。外部I/Oは`infrastructure`とWorkflow portへ、再利用可能な判断・変換はContextの`step`へ置く。Stepへclientやinterfaceを注入しない。
