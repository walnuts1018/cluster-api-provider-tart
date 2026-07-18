# ADR 0007: UEFI、Legacy BIOS、Raspberry Piを別platform profileとして扱う

- Status: Accepted
- Date: 2026-06-28

## Context

ESPだけを強制する構成はLegacy BIOSを持つ古いPCで起動できない。Raspberry Pi 4/5は一般的なPC UEFI/iPXEと異なるfirmware、TFTP、boot partition、EEPROM設定を持つ。DHCP Option 93でarm64を識別できることは、Raspberry Piのboot互換性を意味しない。

## Decision

- Profile IDを`<architecture>-<firmware>-<layout>/v<schema>`形式とする。
- 最低限`amd64-uefi-ab/v1`、`amd64-bios-ab/v1`、`arm64-uefi-ab/v1`を別Profileとする。
- Raspberry Piは`raspberrypi4-eeprom-ab/v1`と`raspberrypi5-eeprom-ab/v1`を分ける。
- Profileはarchitecture、最低CPU level、Firmware、Boot Transport、必須Capability、Disk Role、mount path、bootloader、Agent Artifact digest、Health Gateを必須fieldとする。
- Host allocation時にProfileの必須fieldとTartHost Spec/Statusを全て照合する。1項目でも不一致なら候補から除外する。
- 初期実用リリースは`amd64-uefi-ab/v1`だけをSupportedとする。

## Consequences

- 対象範囲を偽らず段階的に拡張できる。
- build/test artifact数が増える。
- Legacy BIOSではBIOS boot partitionとGRUBの配置・boot trial検証が追加で必要になる。
- Raspberry Pi対応は「arm64 iPXE対応」の延長ではなく独立タスクになる。

## Alternatives

- 全hardwareへESP layoutを強制する: 要求する旧PCを満たさないため却下。
- Raspberry PiもOption 93だけで分岐する: firmware差を吸収できないため却下。
