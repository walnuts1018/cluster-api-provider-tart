# ADR 0005: 能力別Go interfaceを先に実装し、外部ABIはgRPCを候補とする

- Status: Accepted
- Date: 2026-06-28

## Context

Redfishはpower state、power off、boot override、Virtual Mediaを提供できる場合がある。一方WoLはpower onしか保証できない。全実装へ`PowerOn/PowerOff/GetPowerState/SetBootDevice`を強制すると、unsupportedを通常エラーとして扱う不正確なinterfaceになる。将来のSwitchBot/ESP32等へ拡張したいが、実績のないABIを先に固定すると変更が困難になる。

## Decision

- PowerOn、PowerControl、BootOverride、VirtualMedia等へinterfaceを分割する。
- driverはcapabilityを返し、orchestratorはplan作成前に必要能力を照合する。
- MVPはcontroller内のGo adapterとしてWoLとRedfishを実装する。
- 外部pluginは意味論が安定した後にversioned protobuf/gRPCとして追加する。
- 外部pluginは別processとし、deadline、health、最大request size、認証、監査、circuit breakerを持つ。
- pluginへ管理クラスタ全体のKubernetes credentialを渡さない。

## Consequences

- 部分能力しかないhardwareを正確に扱える。
- operation orchestrationとdriver実装を独立して試験できる。
- SwitchBot等の高遅延・eventual consistencyも同じ非同期operationとして扱える。
- gRPC API追加時にはGo型との変換層が必要になる。

## Rejected alternatives

- 単一の巨大interface: WoLで偽の状態または恒常エラーを生む。
- Go plugin package: Go toolchain/依存ABIが強く結合し、process isolationがない。
- WASMを最初のABIとする: network/credential/async operationのhost capability設計が先に必要で、MVPの不確実性を増やす。

