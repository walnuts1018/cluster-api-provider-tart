# Task 02: APIとDomain再設計

## 目的

Host割当、長時間Operation、A/B slot、削除PolicyをKubernetes Resourceへ保存し、controller再起動後も同じ状態から処理を再開できるようにする。

## 依存

- Task 01
- ADR 0001、0002、0003

### Task 01未完了で先行する範囲

2026-07-04の実装方針として、Task 01の完了を待たず、Task 01の検証結果に依存しない
APIとDomainの実装を先行する。これはTask 02の依存完了またはPhase 1の開始条件達成を
意味しない。

- 先行可能: Accepted ADRで確定したHost lifecycle、Operationの永続状態、
  Capability、Operation ID、削除Policy、排他割当
- 保留: ADR 0002に依存するRuntime Hook固有API、ADR 0003に依存する物理partition
  番号やboot trial実装、Task 01で確定するPlatform Profileの物理layout
- Task 02完了条件: Task 01とADR 0002、0003の完了後に保留事項との整合を再検証する

## 入力

- CAPI v1beta2 InfraCluster/InfraMachine contract
- [TartHost phase](../architecture.md#51-tarthost-phase)
- [TartHostOperation phase](../architecture.md#52-tarthostoperation-phase)
- Platform Profile ID

## 成果物

- `TartCluster`、`TartMachine`、`TartMachineTemplate`のcontract対応
- `TartHost`のidentity、root device hint、Driver設定、consumerRef
- `TartHostOperation` CRD
- defaulting/validation Webhook
- Host/Operation/SlotのDomain型と状態遷移関数
- Host選択・予約Kubernetes Adapter
- v1alpha1 APIと旧flowの削除

## API要件

- `TartHost.spec.consumerRef`はnamespace、name、UIDを持つ。
- root device hintは`/dev/disk/by-id`、serial/WWN、最小byte数を持つ。`/dev/sda`だけの指定を拒否する。
- `TartHostOperation.spec`はOperation ID、type、Host/Machine UID、Plan Digest、desiredObjectsDigest、deadlineを必須とする。
- `TartCluster.spec.artifactPolicy.allowedRegistries`は1件以上を必須とし、各値を`hostname`または`hostname:port`形式で保存する。wildcardとpathを拒否する。
- Updateではtarget slot、Artifact digest、Artifact Generation、update classを必須とする。
- `status.completedSteps`は重複のないsetとして保存する。
- `status.initialization.provisioned`は一度`true`になった後に`false`へ戻さない。
- `InfraMachineTemplate.spec.template.metadata`とCRD contract labelを実装する。
- providerIDはTartMachine controllerだけが書き、Templateから複製しない。

## 受け入れ条件

1. 100 goroutineから同じHostを予約し、1つだけが成功する。
2. architecture、Firmware、disk size、Capability、Profile IDの1項目でも不一致ならHost候補から除外する。
3. Host.consumerRefだけが保存された状態からTartMachine.hostRefを補完する。
4. TartMachine.hostRefが別UIDのconsumerを指す場合、自動上書きせず`AllocationConflict`にする。
5. 1 Hostに2つ目の非terminal Operationを作成できない。
6. Architecture文書にないHost/Operation phase遷移を全て拒否する。
7. Artifact参照がdigest固定でないobjectをAdmissionで拒否する。
8. deadlineなしのOperationをAdmissionで拒否する。
9. v1alpha1 API、変換Webhook、旧flowのcontroller/server実装が残っていない。
10. SSA dry-runがobjectを永続化せず、default結果を返す。
11. providerIDとworkload Node providerIDの不一致を`Ready=False`にする。
12. `allowedRegistries`が空、wildcardを含む、またはpathを含む`TartCluster`をAdmissionで拒否する。
13. `TartHostOperation` の作成時に `desiredObjectsDigest` が定義通り保存されることをテストする。

## 作業単位

Task 02は複数の独立したCRDと、単独で検証可能な受け入れ条件を含むため、次の
作業単位へ分割する。

| 作業単位 | 内容 | 対応する受け入れ条件 |
|---|---|---|
| 02A | Host、Operation、SlotのDomain型と純粋な状態遷移 | 6 |
| 02B | storage API、TartHostOperation CRD、Admission | 5、7、8、12、13 |
| 02C | CAPI v1beta2 contract、defaulting、SSA、v1alpha削除 | 9、10、11 |
| 02D | Host選択、resourceVersionによる排他予約、参照修復 | 1、2、3、4 |
| 02E | 移行手順、生成差分、全受け入れ条件の証跡 | 1から13 |

02AでCapability名とOperation ID型を確定した後にTask 03との共有を開始する。
02Bから02Dは同じstorage API型を使用するため、02B、02C、02Dの順で実施する。

## 完了証跡

- controller-gen実行差分
- Domain table test名と結果
- envtestの予約競合結果
- v1alpha1参照削除の検索結果
- SSA dry-run test結果

## 実装状況

2026-07-04時点の実装状況を示す。テストを追加しただけで未実行の項目は完了に含めない。

| 受け入れ条件 | 状況 | 証跡または残作業 |
|---|---|---|
| 1 | 実装済み | `TestServiceReserveAllowsOneOfOneHundredConcurrentMachinesWithAPIServer`でenvtestのAPI serverに対する100並列予約のうち1件だけが成功することを確認 |
| 2 | 実装済み | `TestMatch`でarchitecture、Firmware、disk size、Capability、Profile ID、labelの不一致を個別に確認 |
| 3 | 実装済み | `TestServiceEnsureMachineHostReferenceRepairsFromConsumerRef` |
| 4 | 実装済み | UID不一致では参照を維持し、v1beta1 Controllerが`Ready=False`、Reason `AllocationConflict`を設定する |
| 5 | 実装済み | Host UIDから決定した同一Resource名へのCreateで並列作成を直列化し、`TestServiceStartAllowsOneConcurrentOperationPerHost`で100並列中1件だけ成功することを確認 |
| 6 | 実装済み | HostとOperationの全Phase組合せtable test |
| 7 | 実装済み | v1beta1 Admissionのdigest固定OCI参照検証 |
| 8 | 実装済み | v1beta1 AdmissionのOperation deadline検証 |
| 9 | 実装済み | `api/v1alpha1`、旧v1alpha controller、旧iPXE bootstrap server、conversion patchを削除。`rg`で旧API importと旧sample参照が残っていないことを確認 |
| 10 | E2E実装済み、未実行 | GitHub Actions用E2EでSSA dry-runの既定値と非永続化を検証。ローカル実行は禁止 |
| 11 | 実装済み | v1beta1 ControllerがCAPI MachineのNode参照からworkload Nodeを取得し、providerID不一致を`Ready=False`へ反映する |
| 12 | 実装済み | 空、wildcard、path、scheme、不正portをAdmission testで拒否 |
| 13 | 実装済み | `TestTartHostOperationPreservesDesiredObjectsDigest` |

v1beta1は唯一のserved/storage versionである。v1alpha1との互換性維持、変換Webhook、旧flowとの共存は廃止した。

## 対象外

- Driver呼び出し
- Agent HTTP API
- disk書き込み
- Platform Profile CRD化

## 関連

- ADR 0001
- Issue #143、#145
