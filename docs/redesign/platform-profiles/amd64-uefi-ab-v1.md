# `amd64-uefi-ab-*/*` v1 layout

## 状態

Task 11では、同じamd64 UEFI A/B disk layoutをOSとdistributionごとのProfile IDへ分ける。
次のいずれかが成立しない場合は既存値を変更せず、対象組み合わせだけ`v2`として置き換える。

- 8 GiBのUbuntu 24.04 ext4 imageがOS-A/OS-Bへ収まる。
- detached dm-verity hash treeが1 GiBのVerity-A/Verity-Bへ収まる。
- ESP 512 MiBで採用bootloaderのA/B entryとboot trial metadataを保持できる。
- State 8 GiBでkubeadmまたはk3s、kubelet identity、Bootstrap markerを保持できる。
- 64 GiB disk上のData容量でTask 01のcontainerd/kubelet起動試験が成立する。

## Initial Credential

このProfileは`IsolatedL2`を前提とする。hardware identityを持たないため、Initial secretを
公開boot経路へ出さず、Provisioning AgentはcontrollerへTLS接続してSession Tokenを受領する。
`TartHost.status.conditions`では`CredentialRequirement=True`、`Reason=IsolatedL2Required`として表示する。

## Host要件

| 項目 | 値 |
|---|---|
| Architecture | `amd64` |
| CPU level | `x86-64-v1` |
| Firmware | UEFI |
| Boot Transport | iPXE |
| Disk最小容量 | 64 GiB |
| 対応logical sector size | 512 byte、4096 byte |
| partition alignment | 1 MiB |

## OS / Distribution Profile

| Profile ID | OS | Distribution | Kubernetes | State paths | Data paths |
|---|---|---|---|---|---|
| `amd64-uefi-ab-ubuntu-24.04-kubeadm/v1` | Ubuntu 24.04 LTS | kubeadm | `v1.36.x` | `/etc/machine-id`, `/etc/tart`, `/etc/kubernetes` | `/var/lib/containerd`, `/var/lib/kubelet`, `/var/lib/etcd` |
| `amd64-uefi-ab-ubuntu-24.04-k3s/v1` | Ubuntu 24.04 LTS | k3s | `v1.36.x` | `/etc/machine-id`, `/etc/tart`, `/etc/rancher/k3s` | `/var/lib/containerd`, `/var/lib/kubelet`, `/var/lib/rancher/k3s` |
| `amd64-uefi-ab-ubuntu-26.04-kubeadm/v1` | Ubuntu 26.04 LTS | kubeadm | `v1.36.x` | `/etc/machine-id`, `/etc/tart`, `/etc/kubernetes` | `/var/lib/containerd`, `/var/lib/kubelet`, `/var/lib/etcd` |
| `amd64-uefi-ab-ubuntu-26.04-k3s/v1` | Ubuntu 26.04 LTS | k3s | `v1.36.x` | `/etc/machine-id`, `/etc/tart`, `/etc/rancher/k3s` | `/var/lib/containerd`, `/var/lib/kubelet`, `/var/lib/rancher/k3s` |
| `amd64-uefi-ab-debian-13-kubeadm/v1` | Debian 13 | kubeadm | `v1.36.x` | `/etc/machine-id`, `/etc/tart`, `/etc/kubernetes` | `/var/lib/containerd`, `/var/lib/kubelet`, `/var/lib/etcd` |
| `amd64-uefi-ab-debian-13-k3s/v1` | Debian 13 | k3s | `v1.36.x` | `/etc/machine-id`, `/etc/tart`, `/etc/rancher/k3s` | `/var/lib/containerd`, `/var/lib/kubelet`, `/var/lib/rancher/k3s` |

## Physical Layout

先頭と末尾に最低1 MiBをGPT metadata用として残す。固定partitionは表のsizeと完全一致させ、
Dataは残り全領域を使用する。AgentはGPT label、type GUID、size、物理順、PARTUUIDの存在を
全て検証してからDisk Roleを解決する。

| 番号 | Disk Role | GPT label | GPT type GUID | size | filesystem |
|---:|---|---|---|---:|---|
| 1 | Boot | `tart-boot` | `c12a7328-f81f-11d2-ba4b-00a0c93ec93b` | 512 MiB | FAT32 |
| 2 | OS-A | `tart-os-a` | `4f68bce3-e8cd-4db1-96e7-fbcaf984b709` | 8 GiB | ext4 image |
| 3 | Verity-A | `tart-verity-a` | `2c7357ed-ebd2-46d9-aec1-23d437ec2bf5` | 1 GiB | raw verity tree |
| 4 | OS-B | `tart-os-b` | `4f68bce3-e8cd-4db1-96e7-fbcaf984b709` | 8 GiB | ext4 image |
| 5 | Verity-B | `tart-verity-b` | `2c7357ed-ebd2-46d9-aec1-23d437ec2bf5` | 1 GiB | raw verity tree |
| 6 | State | `tart-state` | `0fc63daf-8483-4772-8e79-3d69d8477de4` | 8 GiB | ext4 |
| 7 | Data | `tart-data` | `0fc63daf-8483-4772-8e79-3d69d8477de4` | 残り、最低16 GiB | ext4 |

OS/Verityにはsystemd Discoverable Partitions Specificationのamd64 root/root-verity type GUIDを使う。
A/Bの区別は一意なGPT labelで行い、OS起動時のmountはAgentが報告するPARTUUIDを正本にする。

## Agent実装契約

- Provision Planだけが`sfdisk --wipe always --wipe-partitions always`でGPTを作成できる。
- Update Planは`sfdisk --json`による既存レイアウト検証だけを行い、partition tableを変更しない。
- `sfdisk`完了後は`udevadm settle --timeout=30`を待ってからRoleを解決する。
- Agent Artifactには`blockdev`、`sfdisk`、`udevadm`を含める。
- Agent Artifact manifestは`application/vnd.tart.provisioning-agent.v1`とし、digest固定OCI参照、
  `architecture=amd64`、`firmware=UEFI`、OS/distribution込みの`platformProfile`、
  kernel/initrdのSHA-256 digestとsizeを署名対象へ含める。Redfish VirtualMediaで
  起動するArtifactは、追加で`virtualMedia`のSHA-256 digestとsizeを署名対象へ含める。
- controllerへmountするAgent Artifact directoryは`manifest.json`、
  `manifest.signature.json`、`vmlinuz`、`initrd`を持つ。Redfish VirtualMediaを
  使う場合は同じdirectoryへ`virtual-media.iso`を追加する。信頼する公開鍵は
  Artifactとは別のread-only mountからcontrollerへ渡す。
- `virtual-media.iso`は`mise run agent-artifact-virtual-media`で生成し、GRUBから
  `/vmlinuz`と`/initrd`を起動する。iPXEとVirtualMediaはどちらも
  `controller-url`、Host UID、Operation UID、boot MACの4項目だけを
  `tart.agent.*` kernel parameterとして渡す。Initial Credential、Session Token、
  Bootstrap Dataをscriptまたはkernel command lineへ含めない。
- `--prepare-layout-only`は署名済みPlan、deadline、disk identity、許可Roleの検証後だけ実行する。
  Provisionでは選択diskの既存partitionを破壊する診断オプションとして扱う。
