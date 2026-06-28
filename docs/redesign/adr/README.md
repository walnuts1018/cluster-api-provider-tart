# ADR一覧

| ADR | 状態 | 判断 |
|---|---|---|
| [0001](0001-host-and-machine-lifecycles.md) | Accepted | Host inventoryとCAPI Machineのライフサイクルを分離する |
| [0002](0002-capi-in-place-updates.md) | Proposed | インプレース更新はCAPI Runtime SDK Hooksから調整する |
| [0003](0003-ab-slot-layout.md) | Proposed | OS更新単位をA/B filesystem slot imageとする |
| [0004](0004-pull-provisioning-agent.md) | Accepted | Pull型の一時Provisioning Agentを使用する |
| [0005](0005-capability-drivers.md) | Accepted | 能力別Go interfaceを先に実装し、外部ABIはgRPCを候補とする |
| [0006](0006-artifact-and-bootstrap-security.md) | Accepted | digest固定artifactと単回Bootstrap bundleを使用する |
| [0007](0007-platform-profiles.md) | Accepted | UEFI、Legacy BIOS、Raspberry Piを別platform profileとして扱う |
| [0008](0008-stateful-update-classes.md) | Accepted | OS slot切替とState migrationを別トランザクションとして扱う |
| [0009](0009-image-build-toolchain.md) | Proposed | Image Builderを評価するが、最終成果物builderとは未決定とする |
| [0010](0010-common-provisioning-agent.md) | Accepted | BMC搭載機でも共通Provisioning Agentがdiskを書き込む |

`Proposed`は[Task 01](../tasks/01-foundation-spikes.md)または各ADRに記載した検証の完了後に`Accepted`または`Rejected`へ変更する。
