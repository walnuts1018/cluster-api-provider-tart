# Task 02: APIとDomain再設計

## 目的

Host割当、長時間Operation、A/B slot、削除PolicyをKubernetes Resourceへ保存し、controller再起動後も同じ状態から処理を再開できるようにする。

## 依存

- Task 01
- ADR 0001、0002、0003

## 入力

- CAPI v1beta2 InfraCluster/InfraMachine contract
- [TartHost phase](../architecture.md#51-tarthost-phase)
- [TartHostOperation phase](../architecture.md#52-tarthostoperation-phase)
- Platform Profile ID

## 成果物

- `TartCluster`、`TartMachine`、`TartMachineTemplate`のcontract対応
- `TartHost`のidentity、root device hint、Driver設定、consumerRef
- `TartHostOperation` CRD
- defaulting/validation/conversion Webhook
- Host/Operation/SlotのDomain型と状態遷移関数
- Host選択・予約Kubernetes Adapter
- v1alpha1からstorage versionへの移行手順

## API要件

- `TartHost.spec.consumerRef`はnamespace、name、UIDを持つ。
- root device hintは`/dev/disk/by-id`、serial/WWN、最小byte数を持つ。`/dev/sda`だけの指定を拒否する。
- `TartHostOperation.spec`はOperation ID、type、Host/Machine UID、Plan Digest、deadlineを必須とする。
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
9. v1alpha1 fixtureをstorage versionへ変換し、失われるfieldを移行文書へ列挙する。
10. SSA dry-runがobjectを永続化せず、default結果を返す。
11. providerIDとworkload Node providerIDの不一致を`Ready=False`にする。

## 完了証跡

- controller-gen実行差分
- Domain table test名と結果
- envtestの予約競合結果
- conversion前後のYAML fixture
- SSA dry-run test結果

## 対象外

- Driver呼び出し
- Agent HTTP API
- disk書き込み
- Platform Profile CRD化

## 関連

- ADR 0001
- Issue #143、#145
