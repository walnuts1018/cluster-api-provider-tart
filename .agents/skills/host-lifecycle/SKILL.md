---
name: host-lifecycle
description: TartHostのallocation、identity、power、boot、data保持を確認する
when_to_use: TartHost、TartMachineのclaim、hardware discovery、power/boot backend、deletionを実装・レビューする時
---

# Host lifecycle方針

## Hostの寿命

`TartHost`はCAPI Machineより長く存続するinventoryである。Machineの削除やscale downでHost resourceを削除せず、Host上のTalos installation、disk、local persistent dataを保持する。

## registrationとdiscovery

初期登録はMAC addressと必要なpower/boot設定から開始できるようにする。disk UUID、Linux device path、NIC名、system UUIDを事前入力必須にしない。maintenance Talos APIからhardware inventoryを取得し、stable identityとdisk selectorをStatusへ反映する。

inventoryはobserved stateであり、ユーザーが編集するdesired storage modelではない。`/dev/sda`など不安定なdevice nameを基本identityや永続的なselection keyにしない。

## claimとeligibility

明示的なHost referenceを優先し、未指定ならarchitecture、label、capability、failure domain、availabilityを満たすHostからdeterministicに選択する。`Machine.spec.failureDomain`が指定されていれば一致するHostだけを候補にする。

排他claimは`TartHost.spec.consumerRef`をcontroller-managed bindingとしてserver-side applyで管理する。namespace、name、UID、resourceVersionを確認し、既存claimが別Machineを指すHostは上書きしない。`TartHost.status.claimedBy`をlockの正本にせず、Statusには`Claimed` Conditionと観測結果を置く。

Hostのallocation eligibilityは`Available`、`Claimed`、`Retained`、`Reusable`を区別する。`Retained`はworkflow phaseではなく、前回のMachineのdataやTalos identityが残るため自動allocation不可である。`TartHost.spec.reusePolicy`の既定値は`Retain`とし、Machine削除後はclaim解除後も`Retained`にする。ユーザーが`spec.reusePolicy: Reusable`を明示し、安全条件を再確認できるまでselector候補に戻さない。destructiveなreprovision、clean、既存installationのadoptは通常allocationに含めない。

## powerとboot

powerとbootはcapabilityとして扱い、Wake-on-LAN、Redfish、VM API、manual、external network bootを同じResource semanticsへ接続する。具体的なDHCP、TFTP、PXE方式をCRDやdomain modelへ固定しない。

power onの成功はTalos installationの成功を意味しない。maintenance endpoint、expected Hostとのidentity binding、MAC/DHCP、system UUID、inventory、authenticated Talos API、healthを観測するまでprovisionedと判定しない。初期boot assetはsecret-freeを基本とする。

maintenance Talos APIはTLSで暗号化されるが認証済みではない。Hostとendpointのbindingが曖昧ならconfigurationをapplyせずblockedにする。installation後はauthenticated Talos APIへ切り替える。

## deletionと再利用

`TartMachine`削除では、まずCAPIのdrainとcontrol planeのetcd detachを確認し、authenticated Talos APIへshutdown/quiesceを要求する。Hostが停止したことを確認するまでclaimとfinalizerを保持する。APIへ到達できない、停止を確認できない、またはHostが稼働し続けている場合はclaimを解放せず`Blocked`にする。

停止確認後にclaimを解除してもHostは`Retained`としてdataを保持する。cleaning、reprovisioning、disk wipe、force releaseは通常updateや通常deleteの副作用にせず、別の明示的な操作、権限、監査、確認を必要とする。

## MHC

local persistent stateを持つMachineではMachineHealthCheckのdelete-and-recreate remediationを安全な既定値とみなさない。初期運用では対象Machineへ`cluster.x-k8s.io/skip-remediation`を設定し、将来のexternal remediationは同じMachineとHostを維持するpower cycle/Talos recovery方式とする。MHC、rollout、手動削除の全経路でRetained gateとshutdown確認を通す。
