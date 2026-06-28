# ADR 0001: Host inventoryとCAPI Machineのライフサイクルを分離する

- Status: Accepted
- Date: 2026-06-28

## Context

物理ホストは複数世代のCAPI Machineから再利用される。一方、InfraMachineはCAPI Machineと同じライフサイクルを持ち、削除や置換の調整をCAPI controllerが行う。両者を同一リソースで表すと、Machine置換時の物理資産消失、別Machineへの二重割当、削除中のデータ消去判断が曖昧になる。

## Decision

- `TartHost`を長寿命の物理インベントリとする。
- `TartMachine`をCAPI InfraMachine契約に従う短寿命リソースとする。
- 割当はcontrollerが設定する`TartHost.spec.consumerRef`と`TartMachine.status.hostRef`のUID付き相互参照で表す。
- `TartHost.spec.consumerRef`はnamespace、name、UIDの3つを必須とし、UIDが一致しない同名Machineを同じconsumerとして扱わない。
- 同一Hostに対するactive TartMachineは最大1つ、非terminal Operationは最大1つとする。
- API serverのresourceVersionを含むUpdateで予約を確定する。HTTP 409 Conflictを受けた候補は取得済みとみなし、同じlist結果を再利用せず再listする。
- Machine削除時は`WipeAll`、`RetainData`、`RetainState`のいずれかを必須指定する。各Policyの遷移先は[target-state](../target-state.md#7-machine削除とhost再利用)へ固定する。

## Crash recovery

相互参照が片側だけ更新された場合は次の規則で修復する。

1. Host.consumerRefのUIDに一致するTartMachineが存在し、そのTartMachine.hostRefが空ならhostRefを補完する。
2. TartMachine.hostRefが指すHostのconsumerRefが別UIDなら、TartMachine側をReadyにせず`AllocationConflict` Conditionを設定する。
3. consumerRefのUIDに一致するTartMachineが存在しなければ、削除Policyに従うCleaning Operationを作成する。
4. controllerがUID不一致を自動上書きしてはならない。

## Consequences

- 物理在庫をMachineから独立して登録・保守できる。
- A/B更新中も同じHost割当を維持できる。
- HostとMachineの2リソース間にcrash windowが生じるため、Reconcileで片側参照を修復する必要がある。
- Host selectionではarchitecture、firmware、root disk最小size、必須Driver Capability、Platform Profile IDを全て照合する。

## Alternatives

- Machineを物理ホストそのものとして扱う: CAPIの削除・置換と物理資産の寿命が一致しないため却下。
- controller memoryのleaseを使う: 再起動・leader変更で失われるため却下。
