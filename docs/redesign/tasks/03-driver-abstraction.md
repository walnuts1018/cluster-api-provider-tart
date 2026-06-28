# Task 03: Driver抽象化とWoL移行

## 目的

物理操作をcapability別portへ分離し、既存WoLを最初のadapterとして移行する。

## 依存

- Task 01でoperation semanticsが確定していること

## 実装範囲

- PowerOn、PowerControl、BootOverride、VirtualMediaの小さなinterface
- `On/Off/Unknown`のPowerState
- typed BootTargetとUnsupported分類
- driver registryとhost capability照合
- operation IDによる冪等性
- deadline、retry分類、指数バックオフ、circuit breaker境界
- 既存`pkg/wol`のWoL adapter化
- fake driverとcontract test suite

WoL adapterはPowerOnだけを公開する。ping、ARP、agent heartbeatは補助的なreachability observationとして別に扱い、PowerStateを`On`へ偽装しない。

## 受け入れ条件

1. orchestratorが必要能力を持たないdriverを呼ばず、明確なConditionへ変換する。
2. 同じoperation IDの重複PowerOnが安全である。
3. 一時エラー、認証エラー、unsupported、deadlineを区別できる。
4. controller/application packageが`pkg/wol`やRedfish clientへ直接依存しない。
5. fake driverで途中失敗と再開を決定的に試験できる。
6. driver名、operation type、結果のメトリクスにhost名やSecretを含めない。

## 対象外

- gRPC plugin protocol
- SwitchBot/GPIO実装
- Redfish実装

## 関連

- ADR 0005
- Issue #146

