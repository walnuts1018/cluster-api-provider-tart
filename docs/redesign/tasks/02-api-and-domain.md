# Task 02: APIとドメイン再設計

## 目的

CAPI契約、長寿命host、再開可能operation、A/B状態を表現できるAPIと副作用のない状態機械を作る。

## 依存

- Task 01

## 実装範囲

- `TartCluster`、`TartMachine`、`TartMachineTemplate`のv1beta2 contract整合
- `TartHost`のidentity、root device hints、driver設定、capabilities、consumerRef
- 長時間処理を表す`TartHostOperation`
- Host selectionと楽観ロックによる予約
- Operation phase、slot、artifact、attempt、deadline、failureのdomain型
- defaulting/validation/conversion webhook
- 旧v1alpha1 fieldのdeprecationと保存バージョン移行

`TartHostOperation`は少なくともhostRef、machineRef、type、operation ID、target digest、slot、phase、agent acknowledgement、conditionsを持つ。`status.initialization.provisioned`は初期化後にfalseへ戻さず、更新中はConditionで表す。

## 受け入れ条件

1. 2つのMachineが同じhostを同時予約しようとしても1つだけ成功する。
2. architecture、firmware、disk、driver capabilityに不適合なhostを選択しない。
3. HostとMachineの片側参照だけが更新されたcrash状態を修復できる。
4. 1 hostにactive operationを複数作れない。
5. 不正なslot遷移、digestなしartifact、曖昧なdisk targetをWebhookまたはdomainで拒否する。
6. 既存v1alpha1 objectを読み、明示した互換範囲で新storage versionへ変換できる。
7. controller-gen生成物とAPI単体テストが通る。

## テスト

- domain table test
- envtestによる競合、Status patch、finalizer、conversion
- fuzz testによる無効な状態遷移

## 対象外

- 実driver呼び出し
- agent HTTP API
- disk書き込み

## 関連

- ADR 0001
- Issue #143、#145

