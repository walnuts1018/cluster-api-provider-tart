# ADR 0003: OS更新単位をA/B filesystem slot imageとする

- Status: Proposed
- Date: 2026-06-28

## Context

稼働中rootへのパッケージ更新は再現性とrollbackを弱くする。overlayfsはrootをread-writeに見せられるが、書き込み層と新しい下位imageの整合、`dpkg`、initramfs、bootloader更新の責務が複雑になる。whole-disk imageは更新時にState/Dataまで上書きする危険がある。

## Decision

- 初期化時にBoot、OS-A/B、Verity-A/B、State、Dataの論理roleをplatform profileに従って配置する。
- Legacy BIOS profileはBIOS boot partitionを追加する。
- OS成果物はwhole-disk imageではなく、固定slotへ書けるfilesystem imageとする。
- 稼働rootはdm-verityで検証したdeviceをread-onlyでmountし、変更可能パスをState/Dataから明示的にbind mountする。
- 更新はinactive slotだけへ書き、read-back検証後に次回boot targetを変更する。
- boot trialに回数と期限を持たせ、health confirmationがない場合は旧slotへ戻す。
- State schema互換範囲外のimageは書き込み前に拒否する。
- 受理済みgenerationと署名済みverity root hashを照合し、古い正規imageへのrollback攻撃を拒否する。
- UEFIのsystemd-boot boot countingまたは同等方式と、Legacy BIOSのGRUB方式はTask 01で個別に実証し、同一実装を強制しない。
- Secure Boot profileではverity root hashを署名済みUKIまたは署名対象boot metadataへ固定する。Secure Bootなしでは悪意ある同時改変に対する真正性を保証しない。

## Acceptance gate

Task 01で次をQEMU上に実証する。

- Ubuntu 24.04がread-only rootで起動し、kubelet/container runtimeが再起動後も動作する。
- OS blockを改変した場合にdm-verity検証で起動または読み取りが失敗する。
- Bootstrap Provider出力をStateから一度だけ実行できる。
- slot書き込み途中の電源断でactive slotが壊れない。
- 新slotの起動失敗を所定回数で旧slotへrollbackできる。

## Consequences

- 更新とrollbackの境界が明確になる。
- OS image内のmount unitと永続path一覧がdistribution/versionごとの互換契約になる。
- State/Dataのschema migrationはOS rollbackとは別に設計する必要がある。破壊的migrationをcommit前に行ってはならない。
- slotと同容量以上の空きdiskが必要になる。

## Rejected alternatives

- OverlayFS root: 長期の下位image交換とパッケージ状態の一貫性を保証しにくい。
- whole-disk raw imageの上書き: 更新時に永続partitionを保護できない。
- 稼働slotへのin-place package update: rollbackと再現性要件を満たさない。
