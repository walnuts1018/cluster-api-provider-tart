# Task 10: Redfish

## 目的

BMC搭載HostでRedfishを使って電源、次回boot、Virtual Mediaを操作し、WoL/iPXEと同じProvisioning Agentを起動する。

## 依存

- Task 03、06
- ADR 0005、0010

## 入力

- BMC endpoint
- BMC credential Secret参照
- CA bundleまたはSPKI pin
- Operation ID
- Agent Artifact
- Boot Transport選択Policy

## 成果物

- Redfish Power Adapter
- Redfish BootOverride Adapter
- Redfish VirtualMedia Adapter
- Capability discovery
- BMC session/TLS設定
- Redfish simulator Contract Test
- 2種類以上の実機検証記録

## Boot Transport選択

利用者が明示指定しない場合は次の順で選ぶ。

1. `RedfishHTTPBoot`
2. `RedfishVirtualMedia`
3. `RedfishPXE`

利用者が指定したCapabilityをBMCが持たない場合は`Unsupported`で失敗し、別Transportへ自動fallbackしない。

## 実装要件

- Redfish session authenticationを優先し、未対応時だけbasic authenticationへfallbackする。
- TLS certificate検証を既定で有効にする。`insecureSkipVerify` fieldは作成しない。
- Power Offは`GracefulShutdown`と`ForceOff`を別操作とする。
- one-time BootOverrideだけを使用し、通常boot orderを書き換えない。
- Virtual Media mount済みの場合は挿入image digestを比較し、異なるimageを黙って置換しない。
- controller再起動後にBMCをGETして現在状態を再観測する。
- Redfish Adapterはdisk layoutまたはOS installerを実装しない。

## 受け入れ条件

1. BMCが公開するCapabilityだけをTartHost Statusへ保存する。
2. HTTPBoot、VirtualMedia、PXEの各Transportから同じAgent Protocol `/v1`でregisterする。
3. one-time BootOverride後の2回目bootで通常boot orderへ戻る。
4. 同じOperation IDのVirtual Media mountを2回受けても二重mountしない。
5. 異なるOperation/Imageのmount要求をConflictとして拒否する。
6. CA不一致、認証失敗、timeout、Unsupportedを別error型で返す。
7. 認証失敗を再試行しない。
8. Temporary errorは合計3回だけ試行し、4回目を呼ばない。
9. controller再起動後、mount済みmedia、PowerState、BootOverrideを再観測してStatusを修正する。
10. Agentのdisk write code pathがWoL/iPXEと同じpackageである。
11. BMC credential値がlog、Event、Status、traceへ出ない。

## 完了証跡

- Redfish simulator Contract Test
- 実機のvendor/model/BMC Firmware version
- 3 Boot TransportのAgent register log
- one-time boot 2回分のboot順
- TLS/認証/error分類test
- controller再起動前後のBMC/Status比較

## 対象外

- IPMI
- firmware update
- RAID設定
- vendor OS deployment API
- SwitchBot/GPIO

## 関連

- ADR 0005、0010
- Issue #146
