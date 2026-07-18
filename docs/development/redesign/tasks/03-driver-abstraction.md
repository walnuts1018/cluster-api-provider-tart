# Task 03: Driver抽象化とWoL移行

## 目的

controllerのUse CaseからWoL/Redfish固有codeを除き、Hostが持つCapabilityだけを呼び出すPort/Adapter構成へ移行する。

## 依存

- Task 01
- Task 02のOperation ID、Host identity、Capability型
- ADR 0005

### Task 01未完了で先行する範囲

2026-07-05の実装方針として、Task 01の実機・QEMU検証を待たず、Acceptedの
ADR 0005とTask 02で確定した型だけに依存するDriver境界、WoL Adapter、
呼び出しpolicyを先行実装した。boot trialやPlatform Profileの物理layoutには
依存しない。

## 入力

- TartHost management設定
- TartHost StatusのCapability
- Operation ID
- HostTarget（MAC addressまたはBMC endpoint参照）
- context deadline

## 成果物

- PowerOn、PowerOff、ObservePowerState、SetNextBoot、VirtualMedia Port
- Capability set型
- Driver Registry
- `Unsupported`、`AuthenticationFailed`、`Temporary`、`DeadlineExceeded` error型
- 既存`infrastructure/service/wol`を使うWoL Adapter
- 全Driver実装が共有するContract Test
- Fake Driver

## 実装要件

- application/controller packageから`infrastructure/service/wol`を直接importしない。
- WoL Adapterは`PowerOn`だけをRegistryへ登録する。
- MAC addressはparse済み値としてAdapterへ渡し、Adapter内でCRを参照しない。
- 同じOperation IDでWoL packetを再送しても成功として扱う。
- Retryは`Temporary`だけを対象とし、合計3回試行する。1回目は即時、2回目は1秒後、3回目は2秒後とし、productionでは各待機へ±20% jitterを加える。
- Driver callへ30秒deadlineを渡す。
- ping、ARP、Agent heartbeatはPowerStateとは別のReachability値へ保存する。

## 受け入れ条件

1. `PowerOn`がないDriverへPowerOnを要求すると、Driverを呼ばず`Unsupported`を返す。
2. WoL AdapterがPowerOff、ObservePowerState、SetNextBoot、VirtualMedia Capabilityを返さない。
3. 同じOperation IDを2回渡した場合、2回目もerrorにせず、Host状態が1回送信時と同じになる。
4. `Temporary`を3回返すFake Driverの呼び出し間隔が1秒、2秒で、4回目を呼ばない。
5. `AuthenticationFailed`を再試行しない。
6. deadline超過後にgoroutineが残らない。
7. Metric labelがoperation_type、phase、driver、result、rollback以外を含まない。
8. 全Adapterが同じContract Test suiteを通過する。

## 完了証跡

- package依存図または`go list -deps`確認結果
- Fake Driverのerror分類test
- WoL packet送信test
- race detector結果

## 実装状況

2026-07-05時点で、Task 01の検証に依存しない受け入れ条件1から8を実装・検証済み。

| 受け入れ条件 | 証跡 |
|---|---|
| 1 | `TestRegistryRejectsUnsupportedPowerOnBeforeCallingDriver` |
| 2 | `TestAdapterImplementsOnlyPowerOnPort`とRegistryのCapability test |
| 3 | WoL AdapterとFake Driverへ共通のPowerOn Contract Testを適用し、同一Operation IDの再送を確認 |
| 4 | `TestServiceRetriesTemporaryErrorAtDefinedIntervals`で3回呼び出しと1秒、2秒の間隔を確認 |
| 5 | `TestServiceDoesNotRetryAuthenticationFailure` |
| 6 | `TestServiceDeadlineDoesNotLeaveDriverWorkRunning` |
| 7 | `TestServiceMetricUsesOnlyAllowedLabels` |
| 8 | `TestAdapterPowerOnContract`と`TestDriverPowerOnContract` |

実行済みcommand:

```text
MISE_OFFLINE=1 mise exec -- go test -race ./domain/shared/driver ./infrastructure/service/driver ./infrastructure/service/driver/... ./domain/provisioning/workflow/provision_machine -count=1
MISE_OFFLINE=1 mise exec -- go test ./... -v
MISE_OFFLINE=1 mise run build
MISE_OFFLINE=1 mise run lint
MISE_OFFLINE=1 mise run generate
MISE_OFFLINE=1 mise exec -- go list -deps ./domain/provisioning/workflow/provision_machine ./infrastructure/k8s_controller
```

最後の依存検査では`domain/provisioning/workflow/provision_machine`と`infrastructure/k8s_controller`から
`infrastructure/service/wol`への依存がないことを確認した。WoL実装への依存は
`infrastructure/service/driver/wol`だけに存在する。ローカルE2Eは実行していない。

## 対象外

- gRPC plugin
- Redfish Adapter
- SwitchBot/ESP32
- PowerStateのping推測

## 関連

- ADR 0005
- Issue #146
