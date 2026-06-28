# ADR 0004: Pull型の一時Provisioning Agentを使用する

- Status: Accepted
- Date: 2026-06-28

## Context

WoLのみのPCにはBMCや常設management agentがなく、SSH pushは対象OS、認証、ネットワーク到達性へ依存する。Redfish Virtual MediaとiPXEで起動経路は異なるが、disk操作と成果物検証は共通化できる。

## Decision

- iPXEまたはVirtual Mediaから同一の一時Linux環境とagentを起動する。
- agentからcontrollerへ外向きに接続し、operation planをpullする。
- planはhost identity、operation ID、期限、disk constraints、artifact digestへbindingする。
- disk操作はagentだけが行い、controllerは任意device pathへの直接命令を送らない。
- agent報告は冪等とし、controllerは全phaseをKubernetes Statusへ永続化する。
- 初期導入とA/B更新でagentを再利用する。

## Consequences

- BMCの有無とdisk provisioningの実装を分離できる。
- 一時OSのkernel/driverが対象hardwareをサポートする必要がある。
- PXE不可能なRaspberry Piや特殊hardwareには別のboot adapterが必要だが、agent protocolは共有できる。
- agent自己申告だけでMachine Readyにせず、起動後のNode/cluster health確認が別途必要になる。

## Alternatives

- SSH push: OS非依存と到達性要件を満たさないため却下。
- RedfishとWoLで別installerを持つ: disk処理・security・状態遷移が重複するため却下。

