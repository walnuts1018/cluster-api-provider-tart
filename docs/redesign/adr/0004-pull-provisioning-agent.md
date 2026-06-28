# ADR 0004: Pull型の一時Provisioning Agentを使用する

- Status: Accepted
- Date: 2026-06-28

## Context

WoLのみのPCにはBMCや常設management agentがなく、SSH pushは対象OS、認証、ネットワーク到達性へ依存する。Redfish Virtual MediaとiPXEで起動経路は異なるが、disk操作と成果物検証は共通化できる。

## Decision

- iPXE、RedfishPXE、RedfishHTTPBoot、RedfishVirtualMediaから同じAgent Protocolを実装するProvisioning Agentを起動する。
- Agentだけが対象Hostのblock deviceをopenする。controllerとDriverはblock deviceへアクセスしない。
- Agentはcontrollerへ外向きTLS接続し、署名済みPlanを取得する。controllerからAgentへの接続開始は禁止する。
- PlanはOperation UID、Host UID、Plan Digest、deadline、disk serial/WWN/最小size、Artifact digest、許可するtarget Disk Roleを必須fieldとする。
- AgentはPlanに列挙されていないpartitionまたはDisk Roleへ書き込まない。
- progress reportはOperation UID、Plan Digest、agentSequence、completedStepを必須とする。
- 初期ProvisioningとA/B更新で同じbinaryとProtocol versionを使用する。

## Consequences

- BMCの有無とdisk provisioningの実装を分離できる。
- 一時OSのkernel/driverが対象hardwareをサポートする必要がある。
- Raspberry Piは専用Boot Transportを使用するが、Agent Protocol `/v1`は共有する。
- agent自己申告だけでMachine Readyにせず、起動後のNode/cluster health確認が別途必要になる。

## Alternatives

- SSH push: OS非依存と到達性要件を満たさないため却下。
- RedfishとWoLで別installerを持つ: disk処理・security・状態遷移が重複するため却下。
