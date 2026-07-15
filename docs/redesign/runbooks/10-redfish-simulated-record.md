# Task 10: Redfish Simulated Record

この記録はrepository内testとrepository管理のRedfish simulator processによるcontract検証をまとめたものであり、実機BMC検証の代替ではない。

## 実行command

```bash
go test ./internal/adapter/driver/redfish ./internal/application/driver ./internal/controller -v
MISE_OFFLINE=1 mise run test-redfish-contract
```

## 確認する内容

1. session authenticationを優先し、未対応時だけbasic authenticationへfallbackする。
2. HTTPBoot、VirtualMedia、PXEの選択順と明示指定時の`Unsupported`失敗を固定する。
3. one-time BootOverride、VirtualMedia idempotency、異なるOperation/Imageの`Conflict`を検証する。
4. controller再起動後にactive Operationを再reconcileした時も、PowerStateとBootStateを再観測する。
   `internal/controller/tarthostoperation_controller_test.go` の
   `TestTartHostOperationReconcilerはRedfish再起動後の再観測でStatusを上書きする`
   で、stale な `status.powerState` と `status.bootState.virtualMedia` を再観測値で上書きする。
5. 認証失敗は再試行せず、temporary errorだけを上限回数まで再試行する。
6. simulator の debug endpoint で通常 boot order と boot 履歴を観測し、1 回目の reset では override target、2 回目の reset では通常 boot order の先頭 target が使われたことを確認する。
7. 実HTTP/TLS越しに起動したRedfish simulator processに対して、session auth、basic fallback、BootOverride、VirtualMedia stateを確認する。
8. `cmd/provisioning-agent` が、明示flag、kernel command line、VirtualMedia 相当の GRUB、HTTPBoot/PXE 相当の iPXE script から得た4項目を、同じ `/v1/agent/register` request へ落とすことを確認する。

## repository内で確認できる主なtest

- `internal/adapter/driver/redfish/adapter_test.go`
  `TestAdapterDiscoversCapabilitiesWithSessionAuthentication`
  `TestAdapterFallsBackToBasicAuthenticationOnlyWhenSessionIsUnsupported`
  `TestAdapterRejectsAuthenticationFailureWithoutBasicFallback`
  `TestAdapterMountRejectsConflictingMedia`
  `TestAdapterSetsOneTimeBootOverride`
- `internal/adapter/driver/redfish/contract_external_test.go`
  実プロセスのRedfish simulatorへ接続し、TLS検証、session auth、basic fallback、BootOverride、VirtualMediaのcontractと、debug endpoint を使った 1 回目の reset で override target、2 回目の reset で通常 boot order の先頭 target が使われたことを確認する
- `internal/application/driver/service_test.go`
  `TestServiceRetriesTemporaryErrorAtDefinedIntervals`
  `TestServiceDoesNotRetryAuthenticationFailure`
  `TestServicePrepareBootPrefersHTTPThenPXE`
  `TestServicePrepareBootUsesVirtualMediaBeforePXE`
  `TestServicePrepareBootRejectsVirtualMediaPreferenceWithoutArtifactProvider`
- `internal/domain/agentboot/script_test.go`
  `TestBuildScriptはregister入力をkernel引数へ正規化する`
- `hack/agent-artifact-iso/main_test.go`
  `TestVirtualMediaはHTTPBootとPXEと同じregister入力へ収束する`
- `cmd/provisioning-agent/main_test.go`
  `TestBuildRegisterRequestは明示flagの設定をそのまま反映する`
  `TestBuildRegisterRequestはkernelCommandLine由来でも明示flagと一致する`
  `TestBuildRegisterRequestはVirtualMediaとHTTPBootPXEで一致する`
- `internal/controller/tarthostoperation_controller_test.go`
  `TestTartHostOperationReconcilerはPowerOn前にBootStateを観測する`
  `TestTartHostOperationReconcilerはPreparingBoot再開時にBootStateを再観測する`
  `TestTartHostOperationReconcilerはRedfishPreferredBootTransportをPrepareBootへ渡す`

## なお未検証の項目

- HTTPBoot / PXE / VirtualMediaの実機register log
- 実機vendor/model/BMC firmwareごとの差異確認
