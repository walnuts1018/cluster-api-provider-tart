# Task 07: Initial Provisioning Simulated Record

この記録はrepository内testによる疑似検証であり、Task 07の実機boot、controller/Node再起動、GitHub Actions上の`mise run test-provisioning-e2e`の代替ではない。

## 実行command

```bash
go test ./internal/provisioningagent/bootstrap ./internal/adapter/k8s/agentsession ./internal/adapter/k8s/bootreport ./internal/adapter/k8s/operation ./internal/application/initialprovisioning ./internal/controller -v
```

## 確認する内容

1. Bootstrap payload原本は適用成功後だけ削除し、Adapter失敗時は保持する。
2. Bootstrap success markerが同じpayload digestを示す場合は再適用しない。
3. Session Tokenは単回配信で、controller再起動後も同じKubernetes statusから認証を再開できる。
4. Boot report受信後もNode ReadyやproviderIDが不足していれば`AwaitingHealth`を維持する。
5. `WipeAll`、`RetainData`、`RetainState`は定義どおりのHost phaseへ遷移する。

## repository内で確認できる主なtest

- `internal/provisioningagent/bootstrap/service_test.go`
  `TestServiceはBootstrap適用成功後にPayload原本を削除しMarkerだけ残す`
  `TestServiceはAdapter失敗時にPayload原本を残す`
  `TestServiceは同じDigestの成功MarkerがあればBootstrapを再適用しない`
- `internal/adapter/k8s/agentsession/service_test.go`
  controller再起動後の`Authenticate`継続
- `internal/adapter/k8s/bootreport/service_test.go`
  完了boot report受信後の`AwaitingHealth`維持
- `internal/application/initialprovisioning/readiness_test.go`
  bootstrap payload digest不足、providerID不一致の拒否
- `internal/controller/tartmachine_v1beta1_controller_test.go`
  `TestTartMachineV1Beta1ReconcilerKeepsAwaitingHealthUntilNodeIsReady`
- `internal/controller/tarthostoperation_controller_test.go`
  手動`WipeAll`、`RetainData`、`RetainState`のHost phase遷移

## なお未検証の項目

- Cluster/Machine作成からNode Readyまでの実機完走
- Agent登録後、Bundle配信後、Node boot後の再起動point実機検証
- Runtime Extension無効時のCAPI Machine置換E2E
- Wipe系Operationの実機ディスク消去確認
