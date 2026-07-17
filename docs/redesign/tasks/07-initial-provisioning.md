# Task 07: 初期Provisioning

## 目的

CAPI object作成からUbuntu 24.04 kubeadm Nodeが`Ready=True`になるまでを、手動SSH/installer操作なしで完了させる。

## 依存

- Task 02、06

## 入力

- CAPI Machine
- CABPK Bootstrap Secret (`format=cloud-config`)
- TartMachine
- `amd64-uefi-ab/v1`
- `WipeAll`削除Policy

## 成果物

- Host選択・予約Use Case
- Provision Operation orchestrator
- Bootstrap Bundle生成
- cloud-config Bootstrap Adapter
- first-boot systemd unit
- OS boot reportとNode health判定
- providerID/address/Condition更新
- `WipeAll`、`RetainData`、`RetainState` Cleaning Operation

## 状態更新規則

- Agent verify完了時点ではOperation=`BootTrial`、TartMachine Ready=`false`とする。
- Bootstrap成功markerがない場合はReadyにしない。
- Node Ready、providerID一致、期待version一致後だけ`initialization.provisioned=true`にする。
- 初期化完了後は`initialization.provisioned`を`false`へ戻さない。
- Bootstrap payload digestが成功markerと一致する場合は再実行しない。

## 受け入れ条件

1. Cluster/Machine作成からNode Readyまで追加のkubectl/SSH操作なしで完了する。
2. controllerをHost予約後、Agent登録後、Bundle配信後、Node boot後に再起動しても同じOperationを再開する。
3. Agentが同じprogressを再送してもpartition作成とBootstrapを重複実行しない。
4. providerID不一致のNodeをReadyにしない。
5. Bootstrap Adapter失敗時にBootstrap payloadを削除せず、OperationをFailedにする。
6. Session Tokenを保持するSecretは、Bootstrap Bundleの送信完了（Step 10）をもって即座に削除し、Node Readyまで保持しない。
7. Bootstrap payload原本は、OS上での適用が成功した時点で即座に実機ディスクから削除する。
8. `WipeAll`は全logical blockをzero overwriteするかdevice sanitize完了を確認してHost=`Available`にする。
9. `RetainData`はStateを消去しDataを保持してHost=`Retained`にする。
10. `RetainState`はState/Dataを保持してHost=`Detached`にする。
11. `Retained`/`Detached` Hostを通常のHost選択候補に含めない。
12. `Retained`/`Detached` Hostは`WipeAll`完了後にだけ新Machineへ割り当てる。
13. Runtime Extension無効時に通常のCAPI Machine置換が成功する。
14. `machineRef` が空の `WipeAll` タイプの `TartHostOperation` を手動作成したとき、対象ホストの Wipe 処理が走り `Available` に遷移する。
15. `WipeAll` の Operation 実行時、ディスク容量に応じた適切な deadline (タイムアウト) が Plan に設定される。

## 完了証跡

- CAPI object作成からNode ReadyまでのEvent/Condition timeline
- 4再起動pointのtest結果
- Bootstrap成功marker
- providerID比較結果
- 3削除Policy後のpartition/Host Status
- GitHub Actions上の`mise run test-provisioning-e2e`結果

## 実装状況

2026-07-17時点で、初期Provisioning controllerの縦方向スライスと
GitHub Actions上のProvisioning E2Eを実装した。

| 受け入れ条件 | 状況 | 証跡または残作業 |
|---|---|---|
| 1 | CI E2E実装済み、一部疑似Node Ready検証済み | Host予約、Provision Operation作成、OS Artifact Manifest解決、署名済みPlan Secret保存、WoL、iPXE、QEMU上のProvisioning Agent起動、Agent登録までをGitHub Actionsの`E2E Provisioning Test`で継続検証する。`scenario=node-ready-only`ではBootReport相当のOperation statusとworkload Node Readyを管理クラスタ上に再現し、controllerが`TartMachine.status.initialization.provisioned=true`、Operation=`Succeeded`、Host=`Provisioned`へ収束することを検証する。実OS slot起動からNode Readyまでの完走は未達 |
| 2 | 一部CI E2E実装済み | 予約済みHostの再取得、OperationRef保存前の再開、同一Operation deadlineの維持を単体テスト済み。Agent登録まではGitHub ActionsのProvisioning E2Eで確認済み。`scenario=node-ready-only`でNode Ready Gate通過後の収束を検証する。Bundle配信後、Node boot後のcontroller再起動pointは実OS image統合後に追加検証する |
| 3 | 実装済み、一部QEMU検証追加 | Agent progressの冪等化はTask 04/06で実装済み。OS上のBootstrap Adapterはpayload digest一致の成功markerがある場合に再適用しない。BootReport/Operation Statusは成功markerのpayload digestを保持する。OS Artifact workflowの`mise run artifact-test-firstboot-qemu`で、mkosi成果物から起動した実OSのfirst-boot unitがfake Agent APIへBootReportを送ることを検証する。実disk slot/kubelet経由の再送確認は未検証 |
| 4 | 実装済み | `EvaluateReadiness`とTartMachine controllerの単体テストでproviderID不一致時にProvisionedへ遷移しないことを確認 |
| 5 | 実装済み、実機未検証 | Bootstrap Bundle生成時にCABPK Secretの`format=cloud-config`とpayload digestを検証する。OS上のBootstrap Adapter失敗時はpayload原本を保持する単体テストを追加済み。実機挙動だけが未検証 |
| 6 | 実装済み | v1beta1 Agent APIはSession TokenをSecretではなく`TartHostOperation.status`のhash/expiry/consumedで保持し、`ClaimBootstrap`成功時に消費済みへ遷移する。`BootstrapDelivered`で新Sessionからの再取得も拒否 |
| 7 | 実装済み、一部CI/QEMU検証済み | Provisioning Agentに`--apply-bootstrap-only`を追加し、Bundleを一時fileへ保存、local cloud-config adapter成功後にpayload原本を削除し、payload digest/adapter version/適用時刻だけをState markerへ残す処理を実装。mkosi OS imageには`provisioning-agent`、first-boot systemd unit、cloud-config adapterを組み込む。通常CIでは`artifact/mkosi`の契約testとshellcheckで、first-bootがBootstrap適用後にBootReportを送る順序を固定する。OS Artifact workflowではmkosi成果物をQEMU direct bootし、fake Agent APIでBootstrap取得からBootReport到達までを検証する。実disk slotからkubelet Readyまでの統合検証は未完 |
| 8-10 | 実装済み、実機未検証 | controller/application側でDeletionPolicyごとのCleaning Operation、Host phase遷移、WipeAll deadline算出を追加。署名済みCleaning Plan、Agent側device sanitize優先、fallbackのzero overwrite処理を実装済み。実機確認は未完 |
| 11 | 実装済み | allocation domainはAvailable以外を通常選択候補から除外 |
| 12 | CI E2E実装済み | Retained/Detachedを通常選択候補から除外するdomain/application testを追加済み。GitHub Actionsの`scenario=retained-wipe-only`で、全HostをRetained/Detachedにした状態では`TartMachine`へHostRefが付かず、手動`WipeAll`完了後に対象Hostへ再割当されることを検証する |
| 13 | CI E2E実装済み | GitHub Actionsの`E2E Provisioning Test`で、`workflow_dispatch`の`scenario=replacement-only`を使うと`ExtensionConfig`未登録のまま固定Bootstrap Secretを参照する最小`MachineDeployment`をsurge相当で2 replicasへ拡張し、CAPI標準controllerがreplacement candidate `Machine`を作成して別HostのProvisioning Agent登録まで進むことを検証する。その後、元の`Machine`削除要求まで送る。Node Ready後のrolling replacement完走はOS image統合後に追加検証する |
| 14-15 | CI E2E実装済み、実機ディスク消去未検証 | `machineRef=nil` の手動 WipeAll はwebhook/controller/host state machineまで実装済み。`scenario=retained-wipe-only`で手動`WipeAll` Operation作成、Host=`Cleaning`、Operation=`Succeeded`、Host=`Available`への収束を検証する。disk容量別deadline算出、署名済みCleaning Plan、Agent側device sanitize優先、fallbackのzero overwrite処理は実装済み。実機での全logical block消去確認だけが未完 |

実装済みの補助機能:

- TartHost/TartMachine/OS Artifact Manifestから署名済みProvision Planを生成する純粋関数
- RFC 8785 Plan digestとEd25519署名
- Operation所有のimmutable Plan SecretをSSAで保存するKubernetes adapter
- OS boot report、State/Data mount、Bootstrap marker、Node Ready、providerID、Kubernetes versionの完了Gate
- Gate通過後のOperation=`Succeeded`、Host=`Provisioned`、TartMachine=`Provisioned`への再試行可能な収束
- 初期Provisioning完了時に、観測済みKubernetes versionを
  `TartMachine.status.installedDistributionVersion`へ保存する接続
- OS上で動くProvisioning AgentのBootstrap適用モード。`cloud-config` Bundleのpayload原本を一時保存し、local adapter成功後だけ削除する。成功markerにはpayload digest、Machine UID、Operation UID、adapter version、適用時刻を保存する
- BootReport protocolと`TartHostOperation.status.lastBootReport`の`bootstrapPayloadDigest`。`bootstrapApplied=true`の場合はpayload digestが必須で、digestなしではProvisioning完了Gateを通過しない
- OS上で動くProvisioning AgentのBootReport送信モード。State上のBootstrap成功markerを読み、payload digestを`bootstrapPayloadDigest`としてAgent APIへ送信する
- mkosi OS imageのfirst-boot unit。`network-online.target`後かつ`kubelet.service`前に、OS内の`provisioning-agent`を`--apply-bootstrap-only`、続けて`--report-boot-only`で起動する。cloud-config adapterはCABPK payloadをNoCloud datasourceへ置き、`cloud-init`のconfig/final moduleを実行する
- `docs/redesign/runbooks/07-initial-provisioning-simulated-record.md` に、Bootstrap payload削除、単回Session、`AwaitingHealth`維持、Cleaning phase遷移の疑似証跡と、GitHub Actions上のProvisioning E2E証跡を追加した

## 対象外

- A/B更新
- Kubernetes version更新
- k3s
- Redfish

## 関連

- ADR 0001、0004、0006
- Issue #145、#147
