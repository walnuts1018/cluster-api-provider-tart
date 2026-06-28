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
6. 成功後、Session Token SecretとBootstrap payload原本を削除する。
7. `WipeAll`は全logical blockをzero overwriteするかdevice sanitize完了を確認してHost=`Available`にする。
8. `RetainData`はStateを消去しDataを保持してHost=`Retained`にする。
9. `RetainState`はState/Dataを保持してHost=`Detached`にする。
10. `Retained`/`Detached` Hostを通常のHost選択候補に含めない。
11. `Retained`/`Detached` Hostは`WipeAll`完了後にだけ新Machineへ割り当てる。
12. Runtime Extension無効時に通常のCAPI Machine置換が成功する。

## 完了証跡

- CAPI object作成からNode ReadyまでのEvent/Condition timeline
- 4再起動pointのtest結果
- Bootstrap成功marker
- providerID比較結果
- 3削除Policy後のpartition/Host Status
- GitHub Actions上の`mise run test-provisioning-e2e`結果

## 対象外

- A/B更新
- Kubernetes version更新
- k3s
- Redfish

## 関連

- ADR 0001、0004、0006
- Issue #145、#147
