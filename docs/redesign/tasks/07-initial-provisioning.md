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
| 3 | 一部実装 | Agent progressの冪等化はTask 04/06で実装済み。Bootstrap Adapterの実機側成功marker処理は未実装 |
| 4 | 実装済み | `EvaluateReadiness`とTartMachine controllerの単体テストでproviderID不一致時にProvisionedへ遷移しないことを確認 |
| 5-7 | 未実装 | cloud-config Adapter、payload原本削除、Session Token Secret削除の統合が必要 |
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

## 対象外

- A/B更新
- Kubernetes version更新
- k3s
- Redfish

## 関連

- ADR 0001、0004、0006
- Issue #145、#147
