# Task 11: 対応Matrix拡大とRelease

## 目的

Ubuntu 24.04 amd64 UEFI kubeadmで完成した縦方向スライスを、1軸ずつ別のOS、distribution、architecture、Firmwareへ移植し、Supported Matrixを公開する。

## 依存

- Task 09、10

## 入力

- Ubuntu 24.04 amd64 UEFI kubeadmのSupported Platform Profile
- OS/Agent Artifact Build pipeline
- Agent Protocol `/v1`
- 初期Provisioning、OSOnly、Kubernetes LifecycleのE2E suite
- 対象OS/board/BMCの公式仕様

## 作業単位の分割

次の各行を独立した作業単位とする。複数行を1つのPRへ含めない。

| 順序 | 追加する軸 | 比較元 |
|---|---|---|
| 1 | Debian 13 | Ubuntu 24.04 |
| 2 | Ubuntu 26.04 | Ubuntu 24.04 |
| 3 | k3s Bootstrap/Control Plane Provider | kubeadm |
| 4 | amd64 Legacy BIOS | amd64 UEFI |
| 5 | arm64 UEFI | amd64 UEFI |
| 6 | Raspberry Pi 4 EEPROM | arm64 UEFI |
| 7 | Raspberry Pi 5 EEPROM | Raspberry Pi 4 |

## 成果物

各作業単位で次を作成する。

- version付きPlatform Profile
- OS/Agent Artifact
- State/Data path一覧
- Bootstrap AdapterまたはProvider統合
- bootloader/Boot Transport Adapter
- Supported/Experimental Matrix更新
- backup/restore/Recovery Runbook
- E2E証跡

## 初期Release Matrixの基準

Task 09 完了直後の release candidate では、更新系の公開状態を少なくとも次のように整理する。

| 項目 | 対象 | 状態 | 追加条件 |
|---|---|---|---|
| 初期Provisioning | Ubuntu 24.04 amd64 UEFI kubeadm | Supported | Task 07/10 の release gate を満たす |
| OSOnly更新 | worker | Experimental | feature gate 必須。Task 08 の failure injection/E2E 継続実行前 |
| KubernetesBinary更新 | worker | Experimental | feature gate 必須。Task 09 の実機/E2E 継続実行前 |
| KubernetesBinary更新 | 3台以上の control plane | Experimental | feature gate 必須。snapshot/apply/recovery の E2E 継続実行前 |
| KubernetesBinary更新 | 単一 control plane | Experimental | management API 停止中の復帰を含む E2E 成功まで維持 |

Supported へ変更する時は、対象行ごとに `target-state.md` と Release Note を同じ変更で更新する。

### 2026-07-17 時点の docs 実装状況

- `docs/release/release-matrix.yaml` を Supported/Experimental Matrix の正本として追加した。
- 人間向けの参照文書として `docs/release/README.md` を追加し、既存 runbook への導線を定義した。
- `docs/release-notes/unreleased.md` を追加し、現行 release candidate の公開状態と既知制約を記録した。
- この変更は release candidate の公開導線だけを追加する。Supported 昇格判定や追加 platform の完了証跡は未実装であり、各受け入れ条件の消化は今後の作業で継続する。

## 受け入れ条件

### 全作業単位共通

1. 初期ProvisioningでNode Readyになる。
2. controller、Agent、Hostの再起動からOperationを再開する。
3. OSOnly更新が成功する。
4. 新slot boot失敗3回で旧slotへRollbackする。
5. `WipeAll`、`RetainData`、`RetainState`が定義どおりのHost phaseになる。
6. unsupportedなOS/architecture/Firmware組合せをAdmissionまたはPlan作成前に拒否する。
7. Artifact signature、digest、dm-verity改変testを通過する。
8. Platform Profileの全State/Data pathが実OSのwrite pathを覆う。

### 軸固有

### Debian 13

- amd64 UEFI kubeadmの全共通条件を通過する。
- Debian package repositoryとversionをlockする。

### Ubuntu 26.04

- x86-64-v1でbuildする。
- Intel Sandy Bridge実機またはQEMU `-cpu SandyBridge`でbootする。
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

### Release Noteに必ず記載する既知制約

- single control plane の `KubernetesBinary` 更新は Experimental のままとし、feature gate なしでは受理しない。
- 上記対象は management API outage を含む controller 再接続 E2E が未完了である間、Supported に昇格しない。
- `StateMigration` の自動復旧は未提供であり、`RecoveryRequired` 到達時は Runbook ベースの手動復旧が必要。

## 完了証跡

- Matrix各行のArtifact digest
- E2E run URL
- hardware/firmware一覧
- backup/restore Runbook実行記録
- migration tool結果
- Release Note

### 2026-07-17 時点で repository 内へ追加済みの公開導線

- Matrix 正本: `docs/release/release-matrix.yaml`
- 人間向け Matrix: `docs/release/README.md`
- Release Note: `docs/release-notes/unreleased.md`

## 対象外

- 任意Linux distribution
- 全Redfish vendor保証
- WASM runtime
- Storage application自体の整合性Snapshot

## 関連

- ADR 0002、0003、0005、0007、0009、0010
- Issue #143、#146、#147
