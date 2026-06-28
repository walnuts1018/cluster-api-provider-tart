# ADR 0001: Host inventoryとCAPI Machineのライフサイクルを分離する

- Status: Accepted
- Date: 2026-06-28

## Context

物理ホストは複数世代のCAPI Machineから再利用される。一方、InfraMachineはCAPI Machineと同じライフサイクルを持ち、削除や置換の調整をCAPI controllerが行う。両者を同一リソースで表すと、Machine置換時の物理資産消失、別Machineへの二重割当、削除中のデータ消去判断が曖昧になる。

## Decision

- `TartHost`を長寿命の物理インベントリとする。
- `TartMachine`をCAPI InfraMachine契約に従う短寿命リソースとする。
- 割当はcontrollerが設定する`TartHost.spec.consumerRef`と`TartMachine.status.hostRef`のUID付き相互参照で表す。
- 同一hostに対する割当・operationは同時に1つだけ許可する。
- API serverの楽観ロックで予約を確定し、Conflictは別候補選択または再試行とする。
- Machine削除時のrelease、clean、data retentionを明示したdeletion policyに従う。

## Consequences

- 物理在庫をMachineから独立して登録・保守できる。
- A/B更新中も同じHost割当を維持できる。
- HostとMachineの2リソース間にcrash windowが生じるため、Reconcileで片側参照を修復する必要がある。
- Host selectionにはarchitecture、firmware、disk、driver capability等の適合判定が必要になる。

## Alternatives

- Machineを物理ホストそのものとして扱う: CAPIの削除・置換と物理資産の寿命が一致しないため却下。
- controller memoryのleaseを使う: 再起動・leader変更で失われるため却下。
