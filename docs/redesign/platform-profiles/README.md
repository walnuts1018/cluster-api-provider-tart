# Platform Profile一覧

Platform Profileはarchitecture、firmware、Boot Transport、Disk Roleの物理配置、bootloader、
Agent Artifactをversion付きで固定する。Profile IDの`vN`は物理disk契約のschema versionであり、
同じIDのpartition順、type GUID、固定sizeを破壊的に変更しない。release candidate の公開状態は
この一覧ではなく [../../release/release-matrix.yaml](../../release/release-matrix.yaml) を正本とする。

| Profile | 状態 | 文書 |
|---|---|---|
| `amd64-uefi-ab-ubuntu-24.04-kubeadm/v1` | Planned | [amd64 UEFI A/B v1](amd64-uefi-ab-v1.md) |
| `amd64-uefi-ab-ubuntu-24.04-k3s/v1` | Planned | [amd64 UEFI A/B v1](amd64-uefi-ab-v1.md) |
| `amd64-uefi-ab-ubuntu-24.04-k0s/v1` | Planned | [amd64 UEFI A/B v1](amd64-uefi-ab-v1.md) |
| `amd64-uefi-ab-ubuntu-26.04-kubeadm/v1` | Planned | [amd64 UEFI A/B v1](amd64-uefi-ab-v1.md) |
| `amd64-uefi-ab-ubuntu-26.04-k3s/v1` | Planned | [amd64 UEFI A/B v1](amd64-uefi-ab-v1.md) |
| `amd64-uefi-ab-ubuntu-26.04-k0s/v1` | Planned | [amd64 UEFI A/B v1](amd64-uefi-ab-v1.md) |
| `amd64-uefi-ab-debian-13-kubeadm/v1` | Planned | [amd64 UEFI A/B v1](amd64-uefi-ab-v1.md) |
| `amd64-uefi-ab-debian-13-k3s/v1` | Planned | [amd64 UEFI A/B v1](amd64-uefi-ab-v1.md) |
| `amd64-uefi-ab-debian-13-k0s/v1` | Planned | [amd64 UEFI A/B v1](amd64-uefi-ab-v1.md) |

`amd64-uefi-ab/v1` はTask 06までの暫定IDとしてのみ残す。新しいManifest、Template、Release Matrixでは
OSとdistributionを含むProfile IDを使用する。
