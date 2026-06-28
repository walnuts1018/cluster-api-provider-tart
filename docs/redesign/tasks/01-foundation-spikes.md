# Task 01: 基礎検証とADR確定

## 目的

大量のproduction codeを変更する前に、A/B disk、read-only root、CAPI Runtime Hook、Bootstrap、Artifact Buildの成立可否をQEMUで判定する。

## 依存

なし。

## 固定する検証環境

| 項目 | 値 |
|---|---|
| OS | Ubuntu 24.04 LTS |
| Architecture | amd64 |
| CPU level | x86-64-v1 |
| Firmware | OVMF UEFI |
| Root filesystem | ext4 + dm-verity |
| Disk | 空disk 1台、最低64 GiB |
| Kubernetes | repositoryのCAPI v1.13.1、kubeadm |
| Boot Transport | iPXE相当またはQEMU direct kernel boot |

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
- partition table (`sfdisk --json`または同等出力)
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
