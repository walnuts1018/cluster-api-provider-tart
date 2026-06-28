# ADR 0007: UEFI、Legacy BIOS、Raspberry Piを別platform profileとして扱う

- Status: Accepted
- Date: 2026-06-28

## Context

ESPだけを強制する構成はLegacy BIOSを持つ古いPCで起動できない。Raspberry Pi 4/5は一般的なPC UEFI/iPXEと異なるfirmware、TFTP、boot partition、EEPROM設定を持つ。DHCP Option 93でarm64を識別できることは、Raspberry Piのboot互換性を意味しない。

## Decision

- `architecture`と`firmware/boot platform`を別軸でmodel化する。
- 標準profileを少なくとも`amd64-uefi-ab`、`amd64-bios-ab`、`arm64-uefi-ab`へ分ける。
- Raspberry Piはmodel世代を含む専用profileとboot adapterを持つ。
- Host allocation時にprofile requirementsと実測capabilitiesを照合する。
- profileごとにpartition layout、bootloader、agent kernel、rollback方式、test matrixを定義する。
- 初期実用リリースは`amd64-uefi-ab`だけをsupportedとする。

## Consequences

- 対象範囲を偽らず段階的に拡張できる。
- build/test artifact数が増える。
- Legacy BIOSではBIOS boot partitionとGRUB等の別検証が必要になる。
- Raspberry Pi対応は「arm64 iPXE対応」の延長ではなく独立タスクになる。

## Alternatives

- 全hardwareへESP layoutを強制する: 要求する旧PCを満たさないため却下。
- Raspberry PiもOption 93だけで分岐する: firmware差を吸収できないため却下。

