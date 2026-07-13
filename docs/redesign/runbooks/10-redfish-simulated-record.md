# Task 10: Redfish Simulated Record

この記録はrepository内testによる疑似検証であり、Redfish simulatorの外部contract testや実機BMC検証の代替ではない。

## 実行command

```bash
go test ./internal/adapter/driver/redfish ./internal/application/driver ./internal/controller -v
```

## 確認する内容

1. session authenticationを優先し、未対応時だけbasic authenticationへfallbackする。
2. HTTPBoot、VirtualMedia、PXEの選択順と明示指定時の`Unsupported`失敗を固定する。
3. one-time BootOverride、VirtualMedia idempotency、異なるOperation/Imageの`Conflict`を検証する。
4. controller再起動後にactive Operationを再reconcileした時も、PowerStateとBootStateを再観測する。
5. 認証失敗は再試行せず、temporary errorだけを上限回数まで再試行する。

## repository内で確認できる主なtest

- `internal/adapter/driver/redfish/adapter_test.go`
  `TestAdapterDiscoversCapabilitiesWithSessionAuthentication`
  `TestAdapterFallsBackToBasicAuthenticationOnlyWhenSessionIsUnsupported`
  `TestAdapterRejectsAuthenticationFailureWithoutBasicFallback`
  `TestAdapterMountRejectsConflictingMedia`
  `TestAdapterSetsOneTimeBootOverride`
- `internal/application/driver/service_test.go`
  `TestServiceRetriesTemporaryErrorAtDefinedIntervals`
  `TestServiceDoesNotRetryAuthenticationFailure`
  `TestServicePrepareBootPrefersHTTPThenPXE`
  `TestServicePrepareBootUsesVirtualMediaBeforePXE`
  `TestServicePrepareBootRejectsVirtualMediaPreferenceWithoutArtifactProvider`
- `internal/controller/tarthostoperation_controller_test.go`
  `TestTartHostOperationReconcilerはPowerOn前にBootStateを観測する`
  `TestTartHostOperationReconcilerはPreparingBoot再開時にBootStateを再観測する`
  `TestTartHostOperationReconcilerはRedfishPreferredBootTransportをPrepareBootへ渡す`

## なお未検証の項目

- Redfish simulatorの外部contract test
- HTTPBoot / PXE / VirtualMediaの実機register
- 実機vendor/model/BMC firmwareごとの差異確認
