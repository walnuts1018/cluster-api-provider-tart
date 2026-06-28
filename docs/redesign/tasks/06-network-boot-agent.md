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

## 対象外

- Bootstrap適用
- Node Ready判定
- Redfish
- Legacy BIOS/arm64/Raspberry Pi

## 関連

- ADR 0004、0007
- Issue #147
