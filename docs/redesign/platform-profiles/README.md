# Platform Profile一覧

Platform Profileはarchitecture、firmware、Boot Transport、Disk Roleの物理配置、bootloader、
Agent Artifactをversion付きで固定する。Profile IDの`vN`は物理disk契約のschema versionであり、
同じIDのpartition順、type GUID、固定sizeを破壊的に変更しない。

| Profile | 状態 | 文書 |
|---|---|---|
| `amd64-uefi-ab/v1` | 暫定実装。Task 01のQEMU検証待ち | [amd64 UEFI A/B v1](amd64-uefi-ab-v1.md) |
