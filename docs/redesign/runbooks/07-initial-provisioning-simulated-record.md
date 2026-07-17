# Task 07: Initial Provisioning Simulated Record

この記録はrepository内testとGitHub Actions上のProvisioning E2Eによる検証記録である。
repository内testはTask 07の実機boot、controller/Node再起動、GitHub Actions上の
`mise run test-provisioning-e2e`の代替ではない。

## 実行command

```bash
go test ./artifact/mkosi ./internal/provisioningagent/bootstrap ./internal/adapter/k8s/agentsession ./internal/adapter/k8s/bootreport ./internal/adapter/k8s/operation ./internal/application/initialprovisioning ./internal/controller -v
mise run lint-shell
```

## 確認する内容

1. Bootstrap payload原本は適用成功後だけ削除し、Adapter失敗時は保持する。
2. Bootstrap success markerが同じpayload digestを示す場合は再適用しない。
3. Session Tokenは単回配信で、controller再起動後も同じKubernetes statusから認証を再開できる。
4. Boot report受信後もNode ReadyやproviderIDが不足していれば`AwaitingHealth`を維持する。
5. `WipeAll`、`RetainData`、`RetainState`は定義どおりのHost phaseへ遷移する。
6. mkosi OS imageに含めるfirst-boot unitは、Bootstrap適用後にBootReportを送る順序を維持する。

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
- `artifact/mkosi/firstboot_contract_test.go`
  first-boot unitの起動順、`--apply-bootstrap-only`から`--report-boot-only`への順序、
  NoCloud datasource経由のcloud-config適用契約

## OS image first-boot契約

mkosi buildでは`artifact/mkosi/mkosi.extra`をOS imageへコピーし、
`mise run artifact-build-mkosi`の中でLinux amd64向け`provisioning-agent`を
一時的に`/usr/bin/provisioning-agent`へ配置する。

OS内の`tart-first-boot.service`は`network-online.target`後、`kubelet.service`前に
`/usr/libexec/tart/first-boot`を実行する。first-boot scriptは同じAgent API認証経路で
Bootstrap Bundleを取得してlocal cloud-config adapterを実行し、成功markerを作成した後に
BootReportを送る。通常CIでは次の範囲を検証する。

1. systemd unitが`kubelet.service`より前に実行される。
2. first-boot scriptが`--apply-bootstrap-only`の後に`--report-boot-only`を実行する。
3. BootReportにboot ID、active slot、Artifact generation、State/Data mount状態を渡す。
4. cloud-config adapterがCABPK payloadをNoCloud datasourceへ配置し、`cloud-init`を実行する。
5. `mise run lint`から`shellcheck`を実行し、OS imageへ入るfirst-boot scriptの構文を検証する。

## GitHub Actionsで確認したProvisioning E2E

Workflow: `E2E Provisioning Test`

実行command:

```bash
mise run test-provisioning-e2e
```

確認した範囲:

1. k3s管理クラスタ上へCAPI、CABPK、KCP、Providerを配置する。
2. QEMUでUEFI machineを起動し、dnsmasq、iPXE、kernel、initramfsの順にbootする。
3. initramfs内の実`cmd/provisioning-agent`がDHCP、kernel command line、virtio diskを読み取り、
   controllerのAgent APIへregisterする。
4. CAPI/CABPK/KCP webhook準備を待ってからcluster templateを適用する。
5. `ExtensionConfig`未登録のProvider installを使い、固定Bootstrap Secretを参照する最小
   `MachineDeployment`をsurge相当で2 replicasへ拡張したときにCAPI標準controllerが
   replacement candidate `Machine`を作成し、別HostのProvisioning Agent登録まで進むことを確認する。
   その後、元の`Machine`削除要求まで送る。
6. 全Hostを`Retained`または`Detached`にした状態では通常のHost割当が`NoAvailableHost`で待機し、
   `machineRef=nil`の手動`WipeAll`完了後だけHostが`Available`へ戻り、同じ`TartMachine`へ再割当されることを確認する。
7. `scenario=node-ready-only`では、Agent登録後にBootReport相当のOperation statusとworkload Node Readyを
   管理クラスタ上で再現し、controllerが`TartMachine.status.initialization.provisioned=true`、
   Operation=`Succeeded`、Host=`Provisioned`へ収束することを確認する。

成功したrun:

| 日時 | workflow run | commit |
|---|---:|---|
| 2026-07-16 18:30 UTC | 29524189625 | `38bf639` |
| 2026-07-17 00:23 UTC | 29544626964 | `main` push |
| 2026-07-17 09:06 UTC | 29568145426 | `e03c8a0` (`scenario=replacement-only`) |
| 2026-07-17 09:31 UTC | 29570274422 | `07a29ad` (`scenario=retained-wipe-only`) |

## なお未検証の項目

- Cluster/Machine作成からNode ReadyまでのOS image統合後の完走
- Agent登録後、Bundle配信後、Node boot後の再起動point実機検証
- first-boot unitを含む実OS slotをQEMUで起動し、Bootstrap markerとBootReportがcontrollerへ到達すること
- Wipe系Operationの実機ディスク消去確認

## Node Ready Gateの検証

`workflow_dispatch` では `scenario=node-ready-only` を選ぶと、
`test/e2e/provisioning/provisioning_test.go` の
`Should complete provisioning after BootReport and workload Node Ready gates`
だけを実行できる。

このシナリオは実OS slotからのkubelet起動を証明するものではなく、CI上で次を継続監視する。

1. Agent登録後の`TartMachine`がProviderID、HostRef、OperationRefを持つ。
2. BootReport相当の`lastBootReport`がState/Data mount、Bootstrap marker digestを満たす。
3. controller Pod内から到達できるworkload kubeconfig Secretを使い、Node health observerがNodeを読む。
4. workload Nodeの`spec.providerID`、`Ready=True`、Kubernetes versionがMachineと一致するとProvisioningが完了する。
5. 完了時に`TartMachine`、`TartHostOperation`、`TartHost`がそれぞれProvisioned/Succeededへ収束する。

## Runtime Extension無効時の検証

`workflow_dispatch` では `scenario=replacement-only` を選ぶと、
`test/e2e/provisioning/provisioning_test.go` の
`Should replace a Machine through the default CAPI MachineDeployment path without Runtime Extension`
だけを実行できる。通常の `push` / `pull_request` ではこのテストを含む全件を実行し、
`ExtensionConfig` が未登録のまま CAPI 標準の Machine 置換経路が成立することを継続監視する。

## Retained/Detached Host再利用の検証

`workflow_dispatch` では `scenario=retained-wipe-only` を選ぶと、
`test/e2e/provisioning/provisioning_test.go` の
`Should reallocate a retained or detached Host only after manual WipeAll completes`
だけを実行できる。このシナリオは実diskの全block消去を証明するものではなく、CI上で次を継続監視する。

1. `Retained`/`Detached` Hostを通常のMachine作成で割り当てない。
2. `machineRef=nil`の手動`WipeAll` Operationをcontrollerが受理する。
3. 手動`WipeAll`完了後にHostを`Available`へ戻す。
4. `Available`へ戻ったHostを新しいMachineへ再割当できる。
