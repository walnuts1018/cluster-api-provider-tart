# ADR 0011: Initial Agent CredentialをPlatformの信頼能力で分ける

- Status: Accepted
- Date: 2026-07-05

## Context

Provisioning Agentは空diskのHost上で一時OSとして起動する。iPXEだけを持つ一般的なPCでは、
TPM attestation、事前配置したHost秘密鍵、BMC保護mediaのいずれも利用できない場合がある。
このHostへ秘密値をiPXE script、URL query、kernel command lineで渡すと、DHCP/TFTP/HTTP
server、proxy、process list、kernel logへ露出する。

hardware identityを持たないHostを秘密値だけで強いHost identityへ結び付けることはできない。
この制約を隠して全Platformへ同じ認証強度を表示してはならない。

## Decision

Initial Agent Credential modeを次の4種類に分ける。

| Mode | Initial credential | 必須の信頼境界 |
|---|---|---|
| `MutualTLS` | 事前登録したHost client certificate | private keyをTPMまたはHost固有mediaで保護 |
| `SignedChallenge` | 事前登録したHost keyによるchallenge署名 | private keyをTPMまたはHost固有mediaで保護 |
| `BMCProtectedMedia` | OperationごとにBMC保護mediaへ配置したcredential | BMC認証とmedia access control |
| `IsolatedL2` | Initial secretなし。register後にTLS responseでSession Tokenを受領 | 隔離Provisioning L2、controller証明書のCAまたはSPKI pin、外部から到達不能なlistener |

初期iPXE実装では`IsolatedL2`だけを有効化する。これはHost真正性を提供しない。
`--agent-api-allow-isolated-l2`を明示しない限りAgent APIを起動しない。利用者向けStatusには、
hardware identityなしのProfileが隔離L2を必要とすることを表示する。このStatus実装はTask 04の
残作業とする。

全Modeで次を禁止する。

- credentialまたはSession TokenをURL queryへ入れる。
- credentialまたはSession Tokenを公開iPXE scriptへ入れる。
- credentialまたはSession Tokenをkernel command lineへ入れる。
- Authorization header、Session Token、Bootstrap payloadをaccess log、Event、Status、trace attributeへ記録する。
- 平文HTTP listenerとHTTPSへのredirectを提供する。

`IsolatedL2`のregisterはOperation UID、Host UID、disk inventoryを照合するが、これをhardware
identity検証とは呼ばない。Session TokenはregisterのHTTPS response bodyだけで返し、
10分、認証失敗5回、Bootstrap claimのいずれかで失効する。

## Consequences

- WoL+iPXEだけのHostでも秘密値を公開boot経路へ置かずに開発を進められる。
- `IsolatedL2`では、悪意あるL2参加者が正しいinventoryを模倣する攻撃を防げない。
- productionで一般LANと同じbroadcast domainへAgent APIを公開してはならない。
- TPM、事前登録Host key、BMC保護mediaの実装は同じregister endpointへ追加できる。
- Platform ProfileとTartHost Statusへcredential modeと信頼上の制約を表示する必要がある。

## Alternatives

- iPXE scriptまたはkernel command lineでone-time secretを渡す: logと監視装置への露出を避けられないため却下。
- MAC addressだけをHost identityとして扱う: spoof可能なため却下。
- hardware identity必須としてiPXE-only PCを対象外にする: 初期リリースの対象Hostを満たさないため却下。
