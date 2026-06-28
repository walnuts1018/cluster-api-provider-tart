# 全体の実装計画

## 1. 進め方

機能を横方向に一括実装せず、危険な仮説を先に検証し、Ubuntu 24.04 + amd64 + UEFI + WoL + kubeadmの縦方向スライスを完成させてから対応マトリクスを広げる。

各段階で次のゲートを通す。

- APIと状態遷移が文書化されている。
- 冪等性、競合、途中再開、期限切れの単体テストがある。
- 破壊操作は実機相当環境でfailure injectionを含めて検証されている。
- 既存CAPIの通常置換更新を退行させない。
- CRDやWebhookはKubebuilder/controller-genから生成されている。

## 2. フェーズ

### Phase 0: 技術検証と契約固定

対象: [Task 01](tasks/01-foundation-spikes.md)

- CAPI In-Place Update Hooksの対応バージョンと制約を固定する。
- ext4 slot image、read-only root、State/Data mountをQEMUで起動する。
- UEFIのboot attempt/rollbackを電源断込みで検証する。
- Bootstrap bundleをStateへ配置し、kubeadm bootstrapが完了することを確認する。
- initial Agent credentialのplatform別配送方法と脅威上限を確定する。
- Image Builderのraw変換/role再利用と独自pipelineを比較し、ADR 0009を確定する。

結果が成立しない場合は後続実装を開始せず、ADRを更新する。

### Phase 1: API・ドメイン・driver境界

対象: [Task 02](tasks/02-api-and-domain.md)、[Task 03](tasks/03-driver-abstraction.md)

- v1beta2 contractへ整合したAPIをKubebuilderで設計する。
- Host、Machine、Operation、Slotの状態遷移を純粋関数として実装する。
- Capability別driverとregistryを作り、既存WoLをadapter化する。
- 旧APIからのconversionと移行手順を用意する。

### Phase 2: Agent通信と成果物

対象: [Task 04](tasks/04-agent-protocol.md)、[Task 05](tasks/05-image-pipeline.md)

- versioned agent protocol、session、進捗報告、再開を実装する。
- partition imageとmanifestの再現可能ビルドを作る。
- Image Builderを採用する範囲、または独自pipelineを選ぶ根拠を検証記録に残す。
- digest、署名、SBOM、state schema検証を実装する。

### Phase 3: 初期導入の縦方向スライス

対象: [Task 06](tasks/06-network-boot-agent.md)、[Task 07](tasks/07-initial-provisioning.md)

- 既存ProxyDHCP/TFTP/HTTPからagentを起動する。
- Ubuntu 24.04 + kubeadmをOS-Aへ初期導入する。
- Bootstrap bundleを一度だけ取得・実行する。
- 再起動後にCAPI InfraMachineをprovisionedへ遷移する。

この時点で従来のinstaller/whole-disk flowをdeprecatedにし、移行フラグなしで削除しない。

### Phase 4: OS-only A/B更新

対象: [Task 08](tasks/08-ab-update.md)

- Runtime Hooksをoperation state machineへ接続する。
- inactive slot書き込み、boot trial、health gate、commit/rollbackを実装する。
- worker、複数control-plane、単一ノードcontrol-planeの順に安全性を検証する。
- 更新前バックアップを自動実行するのではなく、policyによる必須preconditionとして検査可能にする。

### Phase 5: Kubernetes Distribution Lifecycle

対象: [Task 09](tasks/09-kubernetes-lifecycle.md)

- CAPI rollout owner（control planeはKCP、workerはMachineDeployment）が決めるversionと順序に従い、node-local adapterでkubeadm upgradeを実行する。
- snapshot、State migration、health確認、復旧境界を実装する。
- k3s用Bootstrap/Control Plane Providerとlifecycle adapterの契約を確定する。

### Phase 6: BMC対応

対象: [Task 10](tasks/10-redfish.md)

- Redfish power、boot override、Virtual Mediaを実装する。
- Virtual Mediaは共通Provisioning Agentのboot transportとして利用する。
- vendor差異と未対応能力をcapability discoveryへ反映する。
- credential rotation、TLS trust、rate limitを検証する。

### Phase 7: 対応範囲拡大とリリース

対象: [Task 11](tasks/11-compatibility-and-release.md)

- k3s、Ubuntu 26.04、Debian 13、Legacy BIOS、arm64を順次有効化する。
- Raspberry Piは専用boot profileの検証後に対応表へ追加する。
- upgrade/rollback matrix、運用runbook、互換性ポリシーを公開する。

## 3. 依存関係

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
                 08 OS-only A/B   10 Redfish
                        |             |
                        v             |
                 09 K8s lifecycle     |
                         \             /
                         v           v
                    11 Compatibility/release
```

Task 04はAPIのoperation modelと成果物manifestを参照するため、02と05の契約部分が先に必要である。実装作業自体は03、04、05を別ブランチで並行化できる。

## 4. リリース単位

| Milestone | 利用可能な成果 | リリース判断 |
|---|---|---|
| M0 | ADRとspike結果 | 主要仮説が実証済み |
| M1 | API/driver/agent protocol | CRD conversionと単体テスト完了 |
| M2 | Ubuntu 24.04初期導入 | 実機で再実行・電源断復旧が成功 |
| M3 | OS-only A/B更新 | workerから開始し、slot rollback成功 |
| M4 | Kubernetes lifecycle | kubeadmの順序、snapshot、health gateを検証 |
| M5 | Redfish | 2系統以上のBMCまたは標準simulatorで検証 |
| M6 | 対応matrix拡大 | 全組合せの証跡とrunbookが存在 |

## 5. 現行Issueとの対応

- Issue #143「ディスクのパーティション設定」: Task 01、02、05、08へ分解する。
- Issue #146「物理操作部分を抽象化」: Task 03、10へ分解する。
- Issue #147「OSインストール手順を変更」: Task 04〜09へ分解する。
- Issue #145「同時に2個TartMachine」: Task 02で予約競合の不変条件として扱う。

既存Issueをそのまま巨大な実装PRにせず、各タスク開始時に親Issueとsub-issueの関係をGitHub上で設定する。

## 6. テスト戦略

### 単体

- 状態遷移、互換性判定、disk selection、artifact検証
- driver capabilityとerror classification
- token expiry、single consumption、operation idempotency
- Runtime Hook patchと更新可否

### 統合

- envtestで予約競合、Status patch、再起動後再開
- fake agent/driverでtimeout、重複report、順序逆転
- QEMUでdisk layout、read-only root、boot trial、rollback

### E2E

- GitHub Actions上だけで`mise run test-e2e`と`mise run test-provisioning-e2e`を実行する。
- 実機labでは電源断、ネットワーク断、破損artifact、BMC timeout、controller再起動を注入する。
- サンプルファイルやWorkflowの存在確認だけを目的とするテストは追加しない。

## 7. 移行と削除

1. 新APIと旧flowをfeature gate下で共存させる。
2. 新規clusterの既定をagent flowへ変更する。
3. 既存`TartMachine`の保存バージョンとStatusを変換する。
4. 少なくとも1リリースのdeprecated期間を設ける。
5. 利用状況とrollback手順を確認した後、installer/whole-disk固有フィールドとコードを削除する。

旧ブランチのスクリプトやworkflowを無条件に取り込まない。whole-disk imageとpartition slot imageは互換ではないため、Task 05で成果物形式から作り直す。

## 8. 実装中に判断を止める条件

- CAPI側が要求するhook順序でquorum-safeなcontrol-plane更新を表現できない。
- read-only rootで対象distributionのセキュリティ更新とBootstrap Provider出力を再現可能に適用できない。
- bootloaderのrollbackがLegacy BIOS/UEFIの対象環境で原子的に扱えない。
- Bootstrap Secretを平文で長期保存しない設計が成立しない。
- driver障害時にoperationの所有権と再開位置を一意に決められない。

この場合は暫定コードを入れず、該当ADRを`Superseded`または`Rejected`へ更新する。
