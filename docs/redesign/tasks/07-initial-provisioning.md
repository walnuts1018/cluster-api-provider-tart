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

2026-07-06時点で、初期Provisioning controllerの最初の縦方向スライスを実装した。

| 受け入れ条件 | 状況 | 証跡または残作業 |
|---|---|---|
| 1 | 一部実装 | Host予約、Provision Operation作成、WoL、Health Gate判定をcontrollerへ接続。OS Artifact Manifest取得とPlan Secret生成の接続、実機bootは未実装 |
| 2 | 一部実装 | 予約済みHostの再取得、OperationRef保存前の再開、同一Operation deadlineの維持を単体テスト済み。Agent登録以降の4再起動pointは未検証 |
| 3 | 一部実装 | Agent progressの冪等化はTask 04/06で実装済み。OS上のBootstrap Adapterはpayload digest一致の成功markerがある場合に再適用しない。BootReport/Operation Statusは成功markerのpayload digestを保持する。実機first-boot unit経由の再送確認は未検証 |
| 4 | 実装済み | `EvaluateReadiness`とTartMachine controllerの単体テストでproviderID不一致時にProvisionedへ遷移しないことを確認 |
| 5 | 一部実装 | Bootstrap Bundle生成時にCABPK Secretの`format=cloud-config`とpayload digestを検証する。OS上のBootstrap Adapter失敗時はpayload原本を保持する単体テストを追加済み。実機挙動は未検証 |
| 6 | 実装済み | v1beta1 Agent APIはSession TokenをSecretではなく`TartHostOperation.status`のhash/expiry/consumedで保持し、`ClaimBootstrap`成功時に消費済みへ遷移する。`BootstrapDelivered`で新Sessionからの再取得も拒否 |
| 7 | 一部実装 | Provisioning Agentに`--apply-bootstrap-only`を追加し、Bundleを一時fileへ保存、local cloud-config adapter成功後にpayload原本を削除し、payload digest/adapter version/適用時刻だけをState markerへ残す処理を実装。OS imageへのadapter実体とfirst-boot systemd unit組み込みは未実装 |
| 8-10 | 未実装 | Cleaning PlanとAgent側disk処理の実装が必要 |
| 11 | 実装済み | allocation domainはAvailable以外を通常選択候補から除外 |
| 12 | 一部実装 | Retained/Detachedを選択しない。WipeAll完了後の再割当E2Eは未検証 |
| 13 | 未検証 | Runtime Extension無効時のCAPI Machine置換E2Eが必要 |
| 14-15 | 未実装 | 手動WipeAllとdisk容量別deadlineの実装が必要 |

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

## 対象外

- A/B更新
- Kubernetes version更新
- k3s
- Redfish

## 関連

- ADR 0001、0004、0006
- Issue #145、#147
