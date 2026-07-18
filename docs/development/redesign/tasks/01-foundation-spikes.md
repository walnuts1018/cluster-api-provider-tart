# Task 01: 基礎検証とADR確定

## 目的

大量のproduction codeを変更する前に、A/B disk、read-only root、CAPI Runtime Hook、Bootstrap、Artifact Buildの成立可否をQEMUで判定する。

## 依存

なし。

## 実装状況

2026-07-06時点で本TaskのQEMU・実機検証は保留している。後続Taskを止めないため、
`amd64-uefi-ab/v1`の物理レイアウトはTask 06で暫定固定した。暫定値と置換条件は
[Platform Profile文書](../platform-profiles/amd64-uefi-ab-v1.md)を正本とする。

Task 01を再開した際は、暫定レイアウトで受け入れ条件を検証し、不成立ならProfile IDを
`amd64-uefi-ab/v2`へ上げる。既存の`v1`レイアウトを黙って変更しない。

2026-07-17時点では、acceptance 8 に関して CI 上で simulator rollback evidence の継続
収集を行っている。これは rollback 判定の入力と状態遷移を固定するための証跡であり、
「3回失敗で旧slotへ戻る」という判定ロジックの継続監視だけを担う。

2026-07-18 時点で、`cmd/os-firstboot-qemu` の direct-kernel QEMU による boot metadata
永続化証跡と、OVMF + systemd-boot を通した `--scenario bootloader-rollback` の 4 回連続
boot 証跡が GitHub Actions で成功した。これにより acceptance 8 の CI 証跡は次の 3 系統で
そろった。

1. rollback 判定系: simulator rollback evidence で、失敗回数と期待状態遷移を固定する。
2. metadata 永続化系: boot metadata 更新直後の強制停止後も、次回 boot で同じ trial 情報を
   再読込できる。
3. boot 選択実証系: `tart-target+3.conf` が 1st から 3rd boot で `+2-1`、`+1-2`、
   `+0-3` へ順に rename され、4th boot で `tart.qemu.boot-entry=rollback` が選択される。

acceptance 8 の CI 証跡は揃ったが、Task 01 全体はまだ他の受け入れ条件を残しているため、
Task 完了とはしない。残る判断は、実機 firmware 差分と slot rootfs 切替の最終照合に限る。

CI で継続収集する証跡の役割分担と、実機差分として残る項目は
[Task 01: Foundation Spikes Simulated Record](../runbooks/01-foundation-spikes-simulated-record.md)
を正本とする。

## 入力

次の値を検証環境の固定入力とする。

| 項目 | 値 |
|---|---|
| OS | Ubuntu 24.04 LTS |
| Architecture | amd64 |
| CPU level | x86-64-v1 |
| Firmware | OVMF UEFI |
| Root filesystem | ext4 + dm-verity |
| Disk | 空disk 1台、最低64 GiB |
| Kubernetes | repositoryのCAPI v1.13.1、kubeadm |
| Boot | disk/OS検証はQEMU direct kernel boot、Network Boot検証はiPXE |

## 成果物

- QEMU VM作成、boot、電源断を実行するmise task
- `amd64-uefi-ab/v1`の暫定Platform Profile
- Disk Role、partition順、type GUID、最小sizeの比較表
- systemd-bootとGRUBのboot trial比較記録
- standard CABPK `cloud-config`適用記録
- Runtime Hook request/response記録
- Image Builder 3案の比較表
- ADR 0002、0003、0009のStatus更新

## 実装要件

- 検証専用codeはproduction packageへ置かない。
- QEMU disk image、download済みArtifact、test credentialをGitへcommitしない。
- mise taskは必要toolのversionを固定する。
- failure injectionは最低限、OS slot書き込み50%時点、boot metadata更新直後、新slot kernel起動直後の3点で実行する。

## 受け入れ条件

1. Ubuntu 24.04がdm-verity rootをread-only mountして起動する。
2. State/Data mount成功後にcontainerdとkubeletを起動する。
3. State mountを失敗させた場合、containerdとkubeletが起動しない。
4. OS blockを1 byte変更した場合、dm-verityがI/O errorを返す。
5. standard CABPK `cloud-config`を1回適用し、再起動後に同じpayloadを再実行しない。
6. OS slot書き込み50%で電源断しても旧Active Slotが起動する。
7. boot metadata更新直後の電源断後、boot trial回数が消失しない。
8. 新slot bootを3回失敗させると旧slotが起動し、4回目に新slotを選択しない。
9. KCPから`CanUpdateMachine`、MachineDeploymentから`CanUpdateMachineSet`が呼ばれる。
10. Runtime Extension再起動後に同じPlan DigestのOperationを重複作成しない。
11. Initial Credential候補ごとに、URL query、公開script、kernel command line、access logへの露出有無を記録する。
12. 3つのArtifact Build案で、build可否、patch行数、build時間、Artifact sizeを記録する。

## 完了証跡

- `mise run <qemu-task>`の全出力
- CIで継続収集したsimulator rollback evidence
- acceptance 8 について、rollback判定入力と状態遷移を示すCI artifact名またはjob名
- acceptance 8 について、4回分のboot順序と各回のslot選択結果を示すQEMU/実機証跡
- `OS Artifact` workflow の `Verify bootloader rollback after three failed target boots`
  job 出力と `dist/os-artifact/bootloader-rollback/*`
- partition table (`sfdisk --json`)
- `findmnt --json`出力
- boot trial 4回分のconsole log
- dm-verity改変test log
- Runtime Hook request/response fixture
- Image Builder比較表

## 対象外

- production CRDの追加
- Redfish実機
- Legacy BIOS
- arm64/Raspberry Pi
- Kubernetes version更新

## 関連

- ADR 0002、0003、0007、0009
- Issue #143、#147
