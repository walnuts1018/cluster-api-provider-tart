# ディレクトリ構成

## 方針

`pkg/`と`internal/`は使わず、bounded contextと外部境界をリポジトリ直下から明示する。`hack/`はKubebuilderが参照する`boilerplate.go.txt`だけに限定し、実行可能な開発ツールは`cmd/`、検証用scriptとfixtureは`test/`へ置く。

Domain Modeling Made Functionalを優先し、技術レイヤーを先に切るのではなく、`domain/<context>`の内側へEntity、Workflow、Step、Event、依存Capabilityをまとめる。CRD、JSON protocol、Kubernetes client、Driver、HTTPなどの外部表現はDomain Entityへ混ぜない。

## 標準構成

```text
api/
  v1beta1/                         # Kubebuilderが生成・管理するCRD API型
artifact/                          # OS Artifact DTOとbuild入力
cmd/
  <binary-or-tool>/
    main.go                        # Composition Rootまたは開発用command
config/                            # Kubebuilder/Kustomize manifest
domain/
  <context>/
    entity/                        # 純粋なEntity、Value Object、状態、Failure
    workflow/
      <verb_noun>/                 # 1 Workflow = 1 package
        workflow.go                # Workflow struct、NewWorkflow、Do
        ports.go                   # このWorkflowが要求するinterface
        outcome.go                 # 必要ならCommand/Eventの閉じた型を分割
    step/                          # 準純粋で再利用可能な関数。DI対象にしない
    event/                         # 複数Workflowで共有するDomain Eventだけ
  shared/
    <concept>/                     # 複数Contextで使うEntity、Value Object
    option/                        # nilをDomainから排除するOption[T]
    result/                        # 期待される失敗を明示するResult[T, F]
dto/
  agent/                           # Agent APIのJSON DTOと署名表現
  agent_artifact/                  # Agent Artifact manifest DTO
infrastructure/
  repository/
    k8s/                           # Kubernetes Resourceを使うdeps実装
  service/
    driver/                        # WoL/RedfishなどのDriver実装
    <external-service>/            # OCI、署名鍵、distribution runtimeなど
  k8s_controller/                  # Reconcile entrypoint
  k8s_webhook/                     # Admission webhook
  http_server/                     # Agent API、boot、Runtime Extension
  provisioning_agent/              # 物理Host側の実行境界
test/
  architecture/                    # 依存方向の静的検査
  fixtures/                        # test fixture
  resource_preservation/           # 横断的な契約検証
  e2e/                             # 実クラスタ相当の縦方向検証
utils/
  <utility>/                       # Domain語彙を持たない汎用処理
  testutils/                       # 複数packageで共有するtest補助
docs/
hack/
  boilerplate.go.txt               # controller-genが要求するための例外
```

全Contextへ空directoryを機械的に作らない。Contextが必要とするdirectoryだけを作る。

## 依存方向

依存は次の向きだけを許可する。

```text
cmd / infrastructure
        |
        v
domain/<context>/workflow/<verb_noun>, step
        |
        v
domain/<context>/entity, domain/shared
```

- `entity`と`domain/shared`は`api`、`dto`、`infrastructure`をimportしない。
- `workflow`はCRDを境界入力として扱ってよいが、Kubernetes clientや具象Driverを直接importしない。
- Workflowが要求する最小Capability interfaceは、そのWorkflow packageの`ports.go`へ置く。Context共通の`deps` packageは作らない。
- Stepは外部I/Oのinterfaceを所有せず、Workflowからpackage関数として直接呼ぶ。I/OはWorkflow packageのportを通す。
- `infrastructure`はWorkflow packageのinterfaceを実装し、外部DTOをSmart Constructorまたはmapping関数でDomain型へ変換する。
- WorkflowはHandlerやEvent Managerを呼び戻さない。Infrastructure境界が返されたEventを解釈し、必要なら次のWorkflowを起動する。

## 型の配置

- 状態ごとに異なるデータは、nullable fieldを持つ1つのstructではなく、状態別structとsealed interfaceで表す。
- optionalなDomain値はpointerではなく`option.Option[T]`を使う。
- 呼び出し元が分岐すべき失敗は`error`ではなく、sealed `Failure`と`result.Result[T, F]`で表す。
- `error`はKubernetes API、HTTP、filesystem、OCI、暗号APIなど外部I/Oの偶発的失敗に限定する。
- JSON/YAML tagは`api`、`dto`、`artifact`などの境界型だけに置き、Domain Entityへ付けない。

## WorkflowとStep

Workflowは`Workflow` structと`Do` methodで定義し、1 Workflowを1 packageへ閉じ込める。

```text
domain/provisioning/workflow/
  provision_machine/
    workflow.go
    ports.go
  complete_provisioning/
    workflow.go
```

- `domain/<context>/workflow/`直下へGoファイルを置かない。
- Workflow packageは`Workflow` structをちょうど1つ持ち、外部へ公開するWorkflow methodは`Do`だけとする。
- `Do`は例外なく`Command`を1つ受け取り、`result.Result[Event, workflow.Failure]`を返す。`error`、CRD、複数引数を公開シグネチャへ直接出さない。
- 登録とBootstrap配信、Provision開始と完了のように、別のCommand・入力・結果を持つ処理は別packageへ分ける。
- `Reconcile`、`Start`、`Register`などの実行methodをWorkflowへ追加しない。ユースケース固有の動詞はpackage名で表す。
- Workflowの依存interfaceは利用側である同じWorkflow packageに置き、実装packageやContext共通のport packageから借りない。

標準形は次のとおりとする。

```go
func (workflow *Workflow) Do(
    ctx context.Context,
    command Command,
) result.Result[Event, sharedworkflow.Failure]
```

Workflowは次のI/O Sandwichだけを組み立てる。

1. package内で定義した依存interfaceから観測値を読む。
2. 境界値を検証済みEntity/Value Objectへparseする。
3. 準純粋Stepを直接呼ぶか、EntityのDecision関数を実行する。
4. `Result`の全variantを処理し、副作用Command/Eventを得る。
5. Workflow自身がport経由で副作用を実行し、Eventを返す。

Workflowの結果を、成功値とfailure pointerを同時に持てるstructで表してはならない。`Allocated OR Pending`のような排他的状態はsealed interface、期待される失敗を伴う値は`Result[T, F]`で表す。

Stepは1つの判断・変換を担う準純粋関数であり、mockやDIの対象にしない。`Executor` struct、`XxxStep interface`、外部clientを保持するStepは禁止する。Workflowのprivate helperが増えた場合、入力と出力だけで表せる判断を`entity`または`step`の関数へ切り出し、I/Oを伴う順序付けはWorkflowへ残す。Command/Eventの分岐を汎用Handler objectへ移してWorkflowを空洞化させない。

## Bounded Contextの粒度

型を置くためだけにbounded contextを作らない。`domain/<context>`は少なくとも1つのWorkflowを所有し、独自のユビキタス言語と状態遷移を持つ単位とする。

本プロジェクトのContextは`provisioning`、`cluster`、`node`、`agentdelivery`を基本単位とする。各Contextは複数のWorkflowを持ち、Entity名ごとにContextを分割しない。

複数ContextやInfrastructure境界から参照される`operation`、`driver`、`slot`、`agentsession`などの型は`domain/shared/<concept>`へ置く。`domain/shared`は便利な雑多packageではなく、意味とinvariantを持つ共有Domain型だけに限定する。
