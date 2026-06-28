# Task 09: Redfish対応

## 目的

BMC搭載rack serverをRedfish経由で電源制御し、PXEまたはVirtual Mediaから共通Agentを起動する。

## 依存

- Task 03、06

## 実装範囲

- PowerState、PowerOn、graceful/forced PowerOff
- one-time BootSourceOverride
- Virtual Media insert/eject
- capability discoveryとvendor差異の隔離
- BMC credential Secret参照
- BMC TLS trust、timeout、retry、rate limit
- simulatorと複数vendor実機のcontract test

IPMIはRedfishで要件を満たせない機器の需要と保守コストを評価してから別adapterとして追加し、Redfish adapter内へ混在させない。

## 受け入れ条件

1. driverが実機の対応能力だけを報告する。
2. one-time boot overrideが通常boot orderを恒久変更しない。
3. operation再実行時にVirtual Mediaの二重mountや予期しないejectを起こさない。
4. self-signedを無条件に許可せず、hostごとの明示trust policyを使う。
5. 認証失敗をretryし続けず、credential値をログへ出さない。
6. Redfish PXE/Virtual Mediaのどちらからでも同じAgent protocolとoperationを使用する。
7. controller再起動後にmount済みmediaとpower stateを再観測して収束する。

## 対象外

- vendor固有firmware update
- RAID構成
- SwitchBot/GPIO

## 関連

- ADR 0005
- Issue #146

