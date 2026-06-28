# Task 11: 対応Matrix拡大とRelease

## 目的

Ubuntu 24.04 amd64 UEFI kubeadmで完成した縦方向スライスを、1軸ずつ別のOS、distribution、architecture、Firmwareへ移植し、Supported Matrixを公開する。

## 依存

- Task 09、10

## Sub-issue分割

次の各行を独立したsub-issueとする。複数行を1つのPRへ含めない。

| 順序 | 追加する軸 | 比較元 |
|---|---|---|
| 1 | Debian 13 | Ubuntu 24.04 |
| 2 | Ubuntu 26.04 | Ubuntu 24.04 |
| 3 | k3s Bootstrap/Control Plane Provider | kubeadm |
| 4 | amd64 Legacy BIOS | amd64 UEFI |
| 5 | arm64 UEFI | amd64 UEFI |
| 6 | Raspberry Pi 4 EEPROM | arm64 UEFI |
| 7 | Raspberry Pi 5 EEPROM | Raspberry Pi 4 |

## 各sub-issueの成果物

- version付きPlatform Profile
- OS/Agent Artifact
- State/Data path一覧
- Bootstrap AdapterまたはProvider統合
- bootloader/Boot Transport Adapter
- Supported/Experimental Matrix更新
- backup/restore/Recovery Runbook
- E2E証跡

## 共通受け入れ条件

1. 初期ProvisioningでNode Readyになる。
2. controller、Agent、Hostの再起動からOperationを再開する。
3. OSOnly更新が成功する。
4. 新slot boot失敗3回で旧slotへRollbackする。
5. `WipeAll`、`RetainData`、`RetainState`が定義どおりのHost phaseになる。
6. unsupportedなOS/architecture/Firmware組合せをAdmissionまたはPlan作成前に拒否する。
7. Artifact signature、digest、dm-verity改変testを通過する。
8. Platform Profileの全State/Data pathが実OSのwrite pathを覆う。

## 軸固有の受け入れ条件

### Debian 13

- amd64 UEFI kubeadmの全共通条件を通過する。
- Debian package repositoryとversionをlockする。

### Ubuntu 26.04

- x86-64-v1でbuildする。
- Intel Sandy Bridge相当CPU modelでbootする。
- systemd version差によるmount/boot unit変更をProfileへ記録する。

### k3s

- 対応Bootstrap/Control Plane Providerを明記する。
- `/etc/rancher/k3s`と`/var/lib/rancher/k3s`のState/Data分割を固定する。
- k3s token/node identityをOSOnly更新後も保持する。

### Legacy BIOS

- BIOS boot partitionとGRUB配置をProfileへ記録する。
- Secure Bootなしのためdm-verityを偶発破損検知と表示する。
- GRUB boot trial 3回とRollbackを実証する。

### arm64 UEFI

- arm64 Agent/OS Artifactを別digestで公開する。
- amd64 ArtifactをHostへ割り当てない。

### Raspberry Pi 4/5

- modelごとにProfileを分ける。
- EEPROM version、onboard Ethernet、firmware partition要件を記録する。
- 汎用UEFI/iPXE Profileを使用しない。

## Release gate

- Supported Matrixの全行に最新release candidateのE2E証跡がある。
- Experimental機能はfeature gateと既知制約をRelease Noteへ記載する。
- migration toolが旧flow利用objectを0件と報告するまで旧field/codeを削除しない。
- Architecture Skill、AGENTS.md、installation、sampleを同じreleaseで更新する。

## 完了証跡

- Matrix各行のArtifact digest
- E2E run URL
- hardware/firmware一覧
- backup/restore Runbook実行記録
- migration tool結果
- Release Note

## 対象外

- 任意Linux distribution
- 全Redfish vendor保証
- WASM runtime
- Storage application自体の整合性Snapshot

## 関連

- ADR 0002、0003、0005、0007、0009、0010
- Issue #143、#146、#147
