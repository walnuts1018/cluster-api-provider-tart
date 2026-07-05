# Task 06: AgentとNetwork Boot統合

## 目的

`amd64-uefi-ab/v1` HostをiPXEから一時OSへ起動し、Provisioning Agentが指定diskだけへOS/Verity payloadを書き込めるようにする。

## 依存

- Task 03、04、05

## 入力

- TartHost identifiers/root device hint
- TartHostOperation Plan
- Agent Artifact digest
- OS Artifact Manifest
- Initial Credential

## 成果物

- UEFI amd64向けiPXE script生成
- Agent Artifact配信
- Provisioning Agentのinventory、disk選択、partition、write、verify実装
- Agent progress/boot report client
- leaderだけがNetwork Serverを起動する処理
- failure injection test

## Disk選択規則

候補diskは次の全条件を満たす必要がある。

1. `/dev/disk/by-id`の期待値と一致する。
2. serialまたはWWNが期待値と一致する。
3. 容量が`minSizeBytes`以上である。
4. Agent自身の一時OSを保持するdeviceではない。

候補が0台または2台以上の場合は書き込みを開始しない。

## 書き込み規則

- 初期Provisioning Planだけがpartition tableを作成できる。
- Update PlanはInactive OS/Verity SlotとBoot trial metadataだけを書き込み可能とする。
- 1 MiB単位で書き込み、最後にblock device全体をfsyncする。
- progressは最低でも10%ごととPhase変更時に送る。
- verify完了前にboot targetを変更しない。

## 受け入れ条件

1. DHCP Option 93がUEFI amd64の場合だけ対象Agent Artifactを返す。
2. BIOS/arm64/未知architecture requestへ対象外応答を返す。
3. disk候補0台/2台の各caseで書き込みsystem callを呼ばない。
4. serial、WWN、sizeの各不一致caseを`DiskIdentityMismatch`で失敗させる。
5. Update PlanでActive SlotまたはState/DataをtargetにするとPlanを拒否する。
6. 50%書き込み時の再起動後、旧Active Slotを変更しない。
7. write完了後、verify前の再起動ではboot targetを変更しない。
8. payload digestまたはverity root hash不一致時にboot targetを変更しない。
9. standby replicaがDHCP/TFTP/HTTPS listenerを開始しない。
10. Agentの書き込み完了だけではTartMachine Readyを`true`にしない。

## 完了証跡

- 各Option 93のDHCP response
- inventory JSON
- disk選択test
- failure injectionごとの最終partition/boot状態
- leader切替時のlistener log

## 実装状況

2026-07-06時点で、誤disk破壊を防ぐ境界、Network Serverの対象制御、Agentがcontrollerへ
接続するまでのruntime境界を先行実装した。
Plan schemaは未リリースのため互換層を設けず、`operationType`、Update時の`activeSlot`、
`rootDevice.deviceName`を直接追加した。

| 受け入れ条件 | 状況 | 証跡または残作業 |
|---|---|---|
| 1 | 実装済み | `TestAgentBootFileSupportsOnlyUEFIAMD64`。Option 93のUEFI amd64だけへx86_64 iPXEまたは二段目script URLを返す |
| 2 | 実装済み | 同testでLegacy BIOS、UEFI arm64、未知値、Option 93欠落を対象外として確認 |
| 3 | 実装済み | `TestServiceDoesNotWriteWhenDiskSelectionFails`で候補0台/2台時のWriter呼出し0回を確認 |
| 4 | 実装済み | `TestSelect`でby-id、serial、WWN、size、Agent一時OS保持deviceの不一致を`DiskIdentityMismatch`として確認 |
| 5 | 実装済み | `TestValidateTargets`と`TestServiceDoesNotWriteUnsafeUpdateTarget`でActive OS/Verity、State、Dataを拒否 |
| 6 | 一部実装 | Updateの書込み先をInactive OS/Verity Slotへ限定し、実block device writerへ接続済み。50% failure injectionと再起動後disk状態の確認が残る |
| 7 | 一部実装 | OCI payloadの1 MiB単位write、fsync、read-back verifyを実block device writerへ接続済み。boot target adapter接続後のfailure injectionが残る |
| 8 | 一部実装 | OCI descriptor、Manifest payload digest、書込み後read-back digestの不一致を失敗させる。verity root hash検証とboot target非変更testが残る |
| 9 | 実装済み、切替試験未実施 | DHCP、TFTP、iPXE/HTTPS、Agent APIのRunnableをleader election対象にした。leader切替時listener logの保存が残る |
| 10 | 実装済み | Agent progressはOperationだけを更新し、TartMachine Readyを変更しない。Node health判定はTask 07 |

実装済みの主な構成:

- `internal/provisioningagent/disk`: 複数identityを全て満たす唯一diskの選択
- `internal/provisioningagent/plan`: Provision/Update別の書込み可能Disk Role検証
- `internal/provisioningagent/payload`: 1 MiB単位write、10%進捗、fsync、read-back digest検証
- `internal/provisioningagent/inventory`: sysfs、`/dev/disk/by-id`、mountinfoからwhole diskと
  Agent一時OS保持deviceを収集
- `internal/provisioningagent/layout`: `amd64-uefi-ab/v1`の1 MiB alignment付きGPT計画、
  label/type GUID/PARTUUIDによるDisk Role解決、Provisionだけに限定した`sfdisk`実行
- `internal/provisioningagent/artifactfetch`: digest固定OCI参照からManifest/署名を先に取得し、
  Ed25519署名、PlanのManifest digest/Artifact Generation、Platform Profile、
  OS/Verity layer descriptorを検証してからpayload streamを公開
- `internal/provisioningagent/writer`: ProvisionではOS-A/Verity-A、UpdateではInactive
  OS/Verity Slotだけを選び、partition容量の事前検証後にpayloadを書込み・read-back検証
- `internal/provisioningagent/client`: HTTPS限定、30秒timeout、最大3回再試行のAgent API client。
  Plan digestとEd25519署名をAgent側で検証
- `internal/provisioningagent/progress`: register responseの保存済みsequenceから再開し、同一requestを
  再試行可能なAgent progress reporter
- `pkg/agentartifact`: Agent Artifactのdigest固定OCI参照、対象architecture/firmware/Profile、
  kernel/initrd descriptor、RFC 8785 canonical JSON、Ed25519署名を検証
- `internal/adapter/k8s/agentboot`: boot MACから`amd64-uefi-ab/v1` Hostと
  `PreparingBoot`から`Verifying`までのactive Operationを解決
- `internal/domain/agentboot`: credentialを含めず、Agent API URL、Host UID、Operation UID、
  boot MACだけを渡すUEFI amd64向けiPXE scriptを生成
- `internal/server/agentboot`: 起動時に署名とpayload digest/sizeを検証し、検証時に開いた
  file descriptorからdigest固定URLでkernel/initrdをHTTPS配信。leaderだけがlistenerを開始
- `internal/provisioningagent.Service`: 全安全検証が成功するまで破壊的Writerを呼ばない実行境界
- `internal/adapter/k8s/agentprogress`: 10%刻みのstep/role/percent、完了Step、最大sequenceを
  Operation Statusへ保存し、Writing/Verifying Phaseへ進める
- `internal/server/bootstrapper`: Option 93の対象判定とleaderだけのDHCP/TFTP起動
- `cmd/provisioning-agent`: register、Plan取得、disk選択までを実行する
  `--preflight-only`診断、GPT作成または既存Role検証だけを行う`--prepare-layout-only`、
  Artifact検証とOS/Verity書込みを行う破壊的な`--write-payloads-only`を提供。
  Registry credentialは任意のDocker互換config fileから読込む

2026-07-06時点で、10%刻みの書込み進捗をAgent APIへ接続し、最新step/role/percentと
Write/Verify完了StepをOperationへ保存する処理まで実装した。再登録時はStatusのagentSequenceを
register responseで返し、Agentが次の番号から再開する。

同日、Agent Artifactの署名・kernel/initrd digest/size検証、固定digest URLによるHTTPS配信、
v1beta1 Host/OperationからのiPXE script生成を実装した。`--agent-artifact-root`を指定すると、
同directoryの`manifest.json`、`manifest.signature.json`、`vmlinuz`、`initrd`、
`public-key.pem`をcontroller起動時に検証する。検証に成功したfile descriptorを保持するため、
検証後のpath差し替えを配信しない。Agent Artifact配信を有効にする場合は
`--agent-artifact-key-id`、`--agent-artifact-base-url`、`--agent-api-url`、
`--agent-boot-cert-file`、`--agent-boot-key-file`も必須とする。

残作業:

- Provisioning Agentを含む実Agent Artifactのbuild/publishとcontroller Podへのread-only mount
- HTTPS対応iPXE binaryから実Agent Artifactを起動し、initramfsが`provisioning-agent`へ
  kernel command lineを渡す実機またはOVMF試験
- verity root hash検証、boot trial metadata更新、failure injection
- 実クラスタでのleader切替試験

## 対象外

- Bootstrap適用
- Node Ready判定
- Redfish
- Legacy BIOS/arm64/Raspberry Pi

## 関連

- ADR 0004、0007
- Issue #147
