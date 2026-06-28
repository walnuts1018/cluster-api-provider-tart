# 全体の実装計画

## 1. 実装順を固定する理由

最初から全OS、全hardware、Kubernetes更新を同時に実装しない。最初の縦方向スライスを次の組合せへ固定する。

```text
Ubuntu 24.04 LTS
+ kubeadm
+ amd64 x86-64-v1
+ UEFI
+ WoL
+ iPXE
+ 初期Provisioning
+ workerのOSOnly A/B更新
```

この組合せでdisk layout、read-only root、Bootstrap、Host割当、更新、Rollbackを実証した後に、1軸ずつ対応範囲を増やす。複数軸を同時に追加すると失敗原因を特定できないため禁止する。

## 2. 全Phase共通の開始条件と終了条件

### 開始条件

- 依存Taskが全て完了している。
- 依存する`Proposed` ADRが`Accepted`または`Rejected`へ更新されている。
- API、Artifact、Protocolのversionが文書に記録されている。
- 変更対象のGitHub Issueと専用branchがある。

### 終了条件

- Task文書の受け入れ条件を1項目ずつ検証した記録がある。
- Go application logicの単体・統合テストが成功している。
- QEMUまたは実機を要求するTaskは、使用したhardware/firmware/image digestを記録している。
- `git diff --check`、ローカルMarkdown link検査、該当mise taskが成功している。
- 設計が変わった場合はADR、Architecture Skill、関連Taskを同じ変更で更新している。

「コードがある」「サンプルが存在する」「手元で一度動いた」だけでは終了条件を満たさない。

## 3. Phase 0: 成立性検証

対象: [Task 01](tasks/01-foundation-spikes.md)

### 入力

- CAPI v1.13.1
- Ubuntu 24.04 amd64
- 空disk 1台を持つQEMU VM
- current/desired TartMachine相当fixture

### 作成する成果物

- QEMU用mise task
- provisional Platform Profile
- partition layout検証記録
- boot trial/rollback検証記録
- Bootstrap cloud-config適用検証記録
- Image Builder比較表
- ADR 0002、0003、0009の更新

### Exit gate

- read-only rootでcontainerd/kubeletが起動する。
- State/Data mount失敗時にkubeletが起動しない。
- disk書き込み中の電源断で旧slotが起動する。
- 新slot boot失敗3回で旧slotへ戻る。
- Runtime Extension再起動後に同じOperationを再開する。

1項目でも成立しない場合はPhase 1へ進まず、該当ADRを`Rejected`または設計変更後の`Proposed`へ更新する。

## 4. Phase 1: API、Domain、Driver境界

対象: [Task 02](tasks/02-api-and-domain.md)、[Task 03](tasks/03-driver-abstraction.md)

### 作成する成果物

- Kubebuilderで追加・更新したCRD
- defaulting/validation/conversion Webhook
- Host/Operationの純粋な状態遷移関数
- resourceVersionを使うHost予約Adapter
- Power/Boot/VirtualMedia Port
- WoL AdapterとFake Driver

### Exit gate

- 100並列の予約試行で1つだけが同じHostを取得する。
- 禁止状態遷移を全てtable testで拒否する。
- WoL DriverがPowerOn以外のCapabilityを報告しない。
- 既存v1alpha1 objectをstorage versionへ変換できる。
- controller packageがWoL/Redfish libraryを直接importしない。

Task 02と03は別branchで並行実装できる。ただし、Capability名とOperation ID型はTask 02で先に確定する。

## 5. Phase 2: Agent ProtocolとArtifact

対象: [Task 04](tasks/04-agent-protocol.md)、[Task 05](tasks/05-image-pipeline.md)

### 作成する成果物

- Agent Protocol `/v1`のrequest/response schema
- Session Token service
- Bootstrap Bundle schema
- OS Artifact Manifest schema
- Ubuntu 24.04 amd64 OS/Verity Artifact
- Provisioning Agent Artifact
- OCI publish用mise task

### Exit gate

- 同じTokenを2並列で使用して1 requestだけがBundleを取得する。
- controller再起動後もOperationとToken hashを復元する。
- 不正signature、digest、size、stateSchemaの各caseをdisk書き込み前に拒否する。
- Artifactのblock改変をdm-verityが検出する。
- Image Builder、role再利用、独自pipelineの比較結果からADR 0009を更新する。

Task 04はTask 02のOperation schemaとTask 05のManifest schemaが確定するまでProtocolをfreezeしない。

## 6. Phase 3: 初期Provisioning

対象: [Task 06](tasks/06-network-boot-agent.md)、[Task 07](tasks/07-initial-provisioning.md)

### 作成する成果物

- iPXEからAgent Artifactを起動するscript生成
- Agentのdisk inventory/selection/write/verify実装
- Bootstrap cloud-config Adapter
- TartMachineからNode ReadyまでのReconcile
- `WipeAll`、`RetainData`、`RetainState`処理

### Exit gate

- CAPI object作成からNode Readyまで手動操作なしで完了する。
- Agent、controller、Hostの各再起動pointから同じOperationを再開する。
- serial/WWN/size不一致のdiskへ1 byteも書き込まない。
- Bootstrap payloadを2回実行しない。
- 各削除Policyが[target-state](target-state.md#7-machine削除とhost再利用)のHost phaseへ遷移する。

Exit gate通過後、新規Clusterの既定をAgent flowへ変更できる。旧flowは最低1リリース残し、deprecated Eventを出す。

## 7. Phase 4: OSOnly A/B更新

対象: [Task 08](tasks/08-ab-update.md)

### 有効化順

1. worker
2. 3台以上のcontrol plane
3. 単一control plane

後の段階は前の段階のfailure injectionを全て通過するまでfeature gateで無効にする。

### Exit gate

- 6種類のCAPI patchの許可/拒否fieldがtable testで固定されている。
- OS Artifact以外の差分をOSOnlyとして覆わない。
- write、verify、boot、healthの各失敗で旧slotへ戻る。
- Rollback後に失敗Artifactを自動再試行しない。
- RuntimeSDK/InPlaceUpdatesを無効にすると通常置換だけが実行される。

## 8. Phase 5: Kubernetes Distribution Lifecycle

対象: [Task 09](tasks/09-kubernetes-lifecycle.md)

### 作成する成果物

- `DistributionLifecycleDriver`
- kubeadm Adapter
- Node Lifecycle Service
- SnapshotRefとLifecycle Stepを持つOperation Status
- worker/control plane別update Plan

### Exit gate

- workerはcontrol plane更新後に`kubeadm upgrade node`を実行する。
- control planeはsnapshot後に`kubeadm upgrade apply`を実行する。
- 各Lifecycle Step直後の再起動でStepを重複実行しない。
- StateMigration失敗を`Succeeded`または自動Rollbackとして報告しない。
- 単一control planeはmanagement API停止中の復帰を含むE2E成功までExperimentalのままとする。

## 9. Phase 6: Redfish

対象: [Task 10](tasks/10-redfish.md)

### 作成する成果物

- Redfish Power、BootOverride、VirtualMedia Adapter
- Capability discovery
- BMC TLS trust設定
- PXE、HTTP boot、Virtual MediaによるAgent起動

### Exit gate

- Redfish Simulatorと2種類以上のBMC実機でContract Testを実行する。
- 未対応Capabilityを`Unsupported`として返す。
- one-time bootが通常boot orderを変更しない。
- controller再起動後にVirtual Mediaの現在状態を再観測する。
- disk書き込みはWoL/iPXEと同じProvisioning Agent code pathを使う。

## 10. Phase 7: 対応Matrix拡大

対象: [Task 11](tasks/11-compatibility-and-release.md)

1つのsub-issueでは1つの軸だけを追加する。順序は次とする。

1. Debian 13 + amd64 UEFI + kubeadm
2. Ubuntu 26.04 + amd64 UEFI + kubeadm
3. k3s Bootstrap/Control Plane Provider統合
4. amd64 Legacy BIOS
5. arm64 UEFI
6. Raspberry Pi 4
7. Raspberry Pi 5

各sub-issueは初期Provisioning、再起動、OSOnly更新、Rollback、削除PolicyのE2Eを持つ。

## 11. 依存関係

```text
01 Foundation spikes
 ├─> 02 API/domain ───────────────┐
 ├─> 03 Driver abstraction ──┐    |
 └─> 05 Image pipeline ──────|────|
                             v    v
                    04 Agent protocol
                             |
                             v
                    06 Network boot agent
                             |
                             v
                    07 Initial provisioning
                         /           \
                        v             v
                 08 OSOnly A/B    10 Redfish
                        |             |
                        v             |
                 09 K8s lifecycle     |
                         \           /
                          v         v
                    11 Compatibility/release
```

## 12. Milestone

| Milestone | 含むTask | 外部から確認するExit gate |
|---|---|---|
| M0 | 01 | A/B、read-only root、RollbackのQEMU証跡 |
| M1 | 02-05 | CRD、Driver、Protocol、ArtifactのContract Test |
| M2 | 06-07 | Ubuntu 24.04実機でNode Ready |
| M3 | 08 | worker OSOnly更新とRollback |
| M4 | 09 | kubeadm worker/control plane更新 |
| M5 | 10 | 2種類のBMCでAgent起動 |
| M6 | 11 | Supported Matrix全組合せのE2E証跡 |

## 13. 現行Issueとの対応

| Issue | 分割先 |
|---|---|
| #143 ディスクpartition設定 | Task 01、02、05、08 |
| #145 TartMachine二重作成疑い | Task 02 |
| #146 物理操作抽象化 | Task 03、10 |
| #147 OS install手順変更 | Task 04-09 |

## 14. テスト実行場所

| Test | Local | GitHub Actions | 実機Lab |
|---|---|---|---|
| Go unit/envtest | 実行 | 実行 | 不要 |
| QEMU disk/boot | 実行 | runner要件を満たす場合 | 不要 |
| `mise run test-e2e` | 禁止 | 実行 | 不要 |
| `mise run test-provisioning-e2e` | 禁止 | 実行 | 不要 |
| WoL/Redfish/power failure | 不要 | 不要 | 実行 |

サンプル、Workflow、scriptの存在だけを確認するテストは禁止する。Go applicationの入力に対する出力または状態遷移を検証する。

## 15. 移行と削除

1. 新APIと旧flowをfeature gate下で共存させる。
2. 新規Clusterの既定をAgent flowへ変更する。
3. 既存objectをstorage versionへ変換する。
4. 1リリースのdeprecated期間中、旧flow利用時にEventを出す。
5. 旧flow利用objectが0件であることを移行toolで確認する。
6. installer/whole-disk固有fieldとcodeを削除する。

feature branch `feat/bare-metal-image-provisioning`のwhole-disk scriptをcherry-pickしてはならない。Task 05で確定したArtifact contractへ必要なpackage設定だけを移植する。

## 16. 実装を停止する条件

次のいずれかが成立した場合は、暫定実装を追加せず該当ADRを更新する。

- CAPI hook順序でCAPI rollout ownerのnode順を維持できない。
- standard CABPK cloud-configをread-only rootとState/Data mount上で適用できない。
- boot trial情報の書き込み中断後に旧slotを選択できない。
- Initial CredentialをURL query、公開script、kernel command line、access logへ出さずに配送できない。
- 同じHostでactive Operationを1つに制限できない。
