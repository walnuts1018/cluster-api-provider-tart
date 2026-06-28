# ADR 0010: BMC搭載機でも共通Provisioning Agentがdiskを書き込む

- Status: Accepted
- Date: 2026-06-28

## Context

Redfish Virtual MediaはBMCがISO等を仮想CD/DVDとして挿入し、PXE/TFTPなしでhostを起動できる。しかしRedfish標準は、任意のA/B partition layoutとdm-verity slotをhost diskへ書く共通APIを定義しない。

Ironicのdirect deployも、deploy ramdisk上のironic-python-agentがimageを取得してtarget diskへコピーする方式である。BMCだけでdisk imageを書き込む方式ではない。Vendor固有のOS deployment機能は機種差が大きく、本Providerのslot/state契約を保証できない。

## Decision

- disk selection、partition作成、OS/Verity書き込み、read-back検証は全Hostで同じProvisioning Agent packageが行う。
- WoL Hostは`IPXE`を使用する。
- BMC HostはCapabilityが存在する順に`RedfishHTTPBoot`、`RedfishVirtualMedia`、`RedfishPXE`を選ぶ。利用者が明示指定した場合は、指定Capabilityがなければfallbackせず失敗する。
- Redfish Driverはboot mode、Secure Boot、one-time boot、Virtual Mediaだけを操作し、OS installerやdisk writerを実装しない。
- Ironicを初期アーキテクチャの必須依存にしない。既存Ironic環境への委譲は将来のexternal provisioning driverとして別途評価する。

## Consequences

- disk layout、security、operation recoveryを1実装で検証できる。
- BMC搭載機でもAgent kernelに対象storage/NIC driverが必要になる。
- Virtual MediaはTFTPを除去できるが、vendor firmware差とimage取得制限をdriverが吸収する必要がある。
- Ironic相当のinspection、cleaning、RAID、firmware機能は自動的には得られない。

## Alternatives

- Vendor OS deployment APIへ直接依存する: portabilityとA/B成果物契約を失う。
- BMC搭載機だけinstaller ISOを起動する: Bootstrap、状態遷移、failure recoveryが二重化する。
- Ironicを必須backendにする: 成熟機能は得られるが、単一Goバイナリ方針に対して大きな運用依存を追加する。

## References

- [Ironic Redfish Virtual Media](https://docs.openstack.org/ironic/latest/admin/drivers/redfish.html#virtual-media-boot)
- [Ironic Direct deploy](https://docs.openstack.org/ironic/latest/admin/interfaces/deploy.html#direct-deploy)
