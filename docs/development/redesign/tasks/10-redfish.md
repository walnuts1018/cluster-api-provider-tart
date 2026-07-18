# Task 10: Redfish

## 目的

BMC搭載HostでRedfishを使って電源、次回boot、Virtual Mediaを操作し、WoL/iPXEと同じProvisioning Agentを起動する。

## 依存

- Task 03、06
- ADR 0005、0010

## 入力

- BMC endpoint
- BMC credential Secret参照
- CA bundleまたはSPKI pin
- Operation ID
- Agent Artifact
- Boot Transport選択Policy

## 成果物

- Redfish Power Adapter
- Redfish BootOverride Adapter
- Redfish VirtualMedia Adapter
- Capability discovery
- BMC session/TLS設定
- Redfish simulator Contract Test
- 2種類以上の実機検証記録

## Boot Transport選択

利用者が明示指定しない場合は次の順で選ぶ。

1. `RedfishHTTPBoot`
2. `RedfishVirtualMedia`
3. `RedfishPXE`

利用者が指定したCapabilityをBMCが持たない場合は`Unsupported`で失敗し、別Transportへ自動fallbackしない。

## 実装要件

- Redfish session authenticationを優先し、未対応時だけbasic authenticationへfallbackする。
- TLS certificate検証を既定で有効にする。`insecureSkipVerify` fieldは作成しない。
- Power Offは`GracefulShutdown`と`ForceOff`を別操作とする。
- one-time BootOverrideだけを使用し、通常boot orderを書き換えない。
- Virtual Media mount済みの場合は挿入image digestを比較し、異なるimageを黙って置換しない。
- controller再起動後にBMCをGETして現在状態を再観測する。
- Redfish Adapterはdisk layoutまたはOS installerを実装しない。

## 受け入れ条件

1. BMCが公開するCapabilityだけをTartHost Statusへ保存する。
2. HTTPBoot、VirtualMedia、PXEの各Transportから同じAgent Protocol `/v1`でregisterする。
3. one-time BootOverride後の2回目bootで通常boot orderへ戻る。
4. 同じOperation IDのVirtual Media mountを2回受けても二重mountしない。
5. 異なるOperation/Imageのmount要求をConflictとして拒否する。
6. CA不一致、認証失敗、timeout、Unsupportedを別error型で返す。
7. 認証失敗を再試行しない。
8. Temporary errorは合計3回だけ試行し、4回目を呼ばない。
9. controller再起動後、mount済みmedia、PowerState、BootOverrideを再観測してStatusを修正する。
10. Agentのdisk write code pathがWoL/iPXEと同じpackageである。
11. BMC credential値がlog、Event、Status、traceへ出ない。

## 完了証跡

- Redfish simulator Contract Test
- 実機のvendor/model/BMC Firmware version
- 3 Boot TransportのAgent register log
- one-time boot 2回分のboot順
- TLS/認証/error分類test
- controller再起動前後のBMC/Status比較

## 対象外

- IPMI
- firmware update
- RAID設定
- vendor OS deployment API
- SwitchBot/GPIO

## 実装状況

### 実装済み

- `domain/shared/driver` に Redfish endpoint、credential、CA bundle、SPKI pin を保持する `RedfishAccess` を追加し、WoL向け `HostTarget` を壊さずに Host 単位の Redfish 接続情報を渡せるようにした。
- `domain/shared/driver` の error 型へ `TLSVerificationFailed` と `Conflict` を追加し、Task 10 が要求する TLS 不一致と mount 競合を通常の temporary error から分離した。
- `infrastructure/service/driver/redfish` に Power、ObservePowerState、one-time BootOverride、Virtual Media mount/unmount、Capability discovery を行う Redfish adapter を追加した。
- `TartHost.spec.management.redfish` に endpoint、CA bundle Secret参照、SPKI pin、preferred boot transport を追加し、CRDへ反映した。
- `infrastructure/repository/k8s/drivertarget` に `TartHost` と Secret から `driverdomain.HostTarget` / `RedfishAccess` を構築する adapter を追加し、`TartHostOperation` controller から利用する接続点を作った。
- `infrastructure/service/driver` に host-aware capability discovery port を追加し、Redfish discovery結果またはregistryの静的capabilityを retry付きで取得できるようにした。
- `infrastructure/repository/k8s/drivercapability` と `infrastructure/repository/k8s/v1beta1host` に、driver discovery結果を `TartHost Status.capabilities` へ保存する adapter を追加し、`TartHostOperation` controller から PowerOn 前に実行するよう接続した。
- `infrastructure/service/driver` と `infrastructure/repository/k8s/driverstate` に PowerState 再観測を追加し、`TartHostOperation` controller が PowerOn 前に BMC から観測した状態を `TartHost.status.powerState` へ保存するようにした。
- `infrastructure/service/driver` に Redfish boot override 準備を追加し、preferred transport が未指定なら `HTTP -> VirtualMedia -> PXE` の順で起動経路を準備し、明示指定時は fallback せずに失敗させる挙動を unit test で固定した。
- `TartHostOperation` controller が Redfish host の `preferredBootTransport` を `PrepareBoot` へ渡し、PowerOn 前に boot override を準備する接続を追加した。
- Agent Artifact manifest と HTTPS配信に optional `virtualMedia` / `virtual-media.iso` を追加し、署名・digest検証後の file descriptor から digest固定URLで配信できるようにした。
- `infrastructure/service/driver` に Agent Artifact provider を追加し、Redfish VirtualMedia選択時にArtifact URLをmountしてから one-time BootOverride を `VirtualMedia` に設定するようにした。未指定時の選択順は `HTTP -> VirtualMedia -> PXE` とした。
- `cmd/controller-manager/main.go` で検証済みAgent Artifactに `virtual-media.iso` がある場合だけ、Redfish VirtualMedia用のArtifact URL providerをdriver serviceへ登録するようにした。
- Redfish adapter で session authentication を優先し、SessionService 未対応時だけ basic authentication へ fallback する挙動を unit test で固定した。
- CA/SPKI pin 検証、Capability discovery、same Operation ID の idempotent mount、異なる mount 要求の `Conflict`、one-time BootOverride を unit test で固定した。
- repository 管理の Redfish simulator に、通常 boot order と各 reset で実際に使われた target を観測できる debug endpoint を追加し、1 回目の reset では override target、2 回目の reset では simulator が保持する通常 boot order の先頭 target が使われたことを確認する contract test を追加した。
- Redfish adapter と `TartHostOperation` controller に BootOverride / VirtualMedia mount 状態の再観測を追加し、controller再起動後に active Operation を再concileした時も `TartHost.status.bootState` と `powerState` をBMC状態で修正できるようにした。
- `cmd/agent-artifact-iso` と `mise run agent-artifact-virtual-media` を追加し、Agent kernel/initrd と公開識別子だけを含む GRUB bootable ISO `virtual-media.iso` を生成できるようにした。Controller URL は HTTPS かつ credential/query/fragment なしだけを許可し、Initial Credential、Session Token、Bootstrap Data を kernel argument へ含めない。
- `cmd/agent-artifact-manifest` と `mise run agent-artifact-manifest` を追加し、`virtual-media.iso` が存在する場合は Agent Artifact manifest の `virtualMedia` descriptor と署名対象へ含めるようにした。
- `cmd/provisioning-agent` に kernel command line を register 入力へ収束させる処理を追加し、`main_test.go` で `tart.agent.*` の4項目が iPXE と VirtualMedia のどちらから来ても共通の register 入力へ正規化されることを固定した。
- `cmd/provisioning-agent` で `/v1/agent/register` request 組み立てを helper へ集約し、明示flag、kernel command line、VirtualMedia 相当の GRUB、HTTPBoot/PXE 相当の iPXE script が最終的に同じ `agentprotocol.RegisterRequest` へ収束することを package test で固定した。
- `docs/development/redesign/runbooks/10-redfish-simulated-record.md` に、repository内testで確認できる疑似contract証跡を追加した。

### 未検証

- Redfish VirtualMediaで起動可能な `virtual-media.iso` の実機boot検証
- HTTPBoot / PXE / VirtualMedia の各 Transport で実際に同じ Agent Protocol `/v1` へ register する実機検証
- 実機検証記録

### 追加の完了証跡（2026-07-16）

- repository 管理の Redfish simulator process を別プロセスで起動し、実HTTP/TLS越しに
  session authentication 優先、SessionService unsupported 時だけ basic fallback、
  one-time BootOverride、VirtualMedia idempotency / conflict、BootState 観測を確認する
  contract test を追加
- repository 管理の Redfish simulator process で、debug endpoint の boot 履歴と通常 boot order を用いて、1 回目の reset では override target、2 回目の reset では通常 boot order の先頭 target が使われたことを確認する contract test を追加
- `infrastructure/k8s_controller/tarthostoperation_controller_test.go` に、Redfish host の active Operation を controller 再起動後に再 reconcile したとき、初期値として入れた stale な `status.powerState` と `status.bootState.virtualMedia` が BMC 観測値で上書きされることを示す test を追加

## 関連

- ADR 0005、0010
- Issue #146
