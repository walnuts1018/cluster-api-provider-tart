---
name: host-lifecycle
description: TartHostのallocation、identity、power、boot、data保持を確認する
when_to_use: TartHost、TartMachineのclaim、hardware discovery、power/boot backend、deletionを実装・レビューする時
---

# Host lifecycle方針

## Hostの寿命

`TartHost`はCAPI Machineより長く存続するmanagement cluster全体で一意なcluster-scoped inventoryである。MAC address、system UUIDなどのstable identityをHost間で重複させない。Machineの削除やscale downでHost resourceを削除せず、Host上のTalos installation、disk、local persistent dataを保持する。

## registrationとdiscovery

初期登録はMAC addressと必要なpower/boot設定から開始できるようにする。disk UUID、Linux device path、NIC名、system UUIDを事前入力必須にしない。maintenance Talos APIからhardware inventoryを取得し、stable identityとdisk selectorをStatusへ反映する。

inventoryはobserved stateであり、ユーザーが編集するdesired storage modelではない。`/dev/sda`など不安定なdevice nameを基本identityや永続的なselection keyにしない。

## claimとeligibility

明示的なHost referenceを優先し、未指定ならarchitecture、label、capability、failure domain、availabilityを満たすHostからdeterministicに選択する。`Machine.spec.failureDomain`が指定されていれば一致するHostだけを候補にする。

排他claimは`TartHost.spec.consumerRef`をcontroller-managed bindingとしてatomic CASで管理する。`GET`でresourceVersionを取得し、consumerRefがnilまたは自分のUIDであることを確認してresourceVersion付きUpdateを行う。JSON Patchの`test`も利用できる。SSAのfield ownershipを分散lockとして使わず、既存claimが別Machineを指すHostは上書きしない。`TartHost.status.claimedBy`をlockの正本にせず、Statusには`Claimed` Conditionと観測結果を置く。

Hostのallocation eligibilityは`Available`、`Claimed`、`Retained`、`Reusable`を区別する。freshなHostは`consumerRef`と`retainedFrom`がなく`Available`である。`Retained`はworkflow phaseではなく、前回のMachineのdataやTalos identityが残るため自動allocation不可である。Machine削除時はcontroller-managedな`TartHost.spec.retainedFrom`へ直前のconsumer UID、namespace、name、cluster UIDを記録する。`TartHost.spec.reusePolicy: Reusable`、現在の`retainedFrom`に一致する`spec.reuseApproval.retainedFromUID`、`spec.reuseMode: Adopt|Reprovision`がそろい、安全条件を再確認できた場合だけ`Reusable`にする。Claim中やfreshな時点の指定を将来の削除承認として扱わない。Cluster secret bundleが失われた後のRetained Hostは`Adopt`不可、`Reprovision`専用である。

## powerとboot

powerとbootはcapabilityとして扱い、Wake-on-LAN、Redfish、VM API、manual、external network bootを同じResource semanticsへ接続する。具体的なDHCP、TFTP、PXE方式をCRDやdomain modelへ固定しない。

power onの成功はTalos installationの成功を意味しない。maintenance endpoint、expected Hostとのidentity binding、MAC/DHCP、system UUID、inventory、authenticated Talos API、healthを観測するまでprovisionedと判定しない。初期boot assetはsecret-freeを基本とする。

maintenance Talos APIはTLSで暗号化されるが認証済みではない。Hostとendpointのbindingが曖昧ならconfigurationをapplyせずblockedにする。installation後はauthenticated Talos APIへ切り替える。

自動Reprovisionを許可するHostは、installed OSからremoteにmaintenance environmentへ戻せるboot strategyをcapabilityとして持つ。Fresh machineのnetwork boot capabilityだけでは自動Reprovisionを許可しない。

## deletionと再利用

`TartMachine`削除では、CAPI Machine controllerのdrainとvolume detachが先に完了し、scale-downのcontrol planeではControl Plane Providerのpre-terminate delete hookがetcd member removalを完了していることを前提にする。その後、authenticated Talos APIへshutdown/quiesceを要求する。Hostが停止したことを確認するまでclaimとfinalizerを保持する。APIへ到達できない、停止を確認できない、またはHostが稼働し続けている場合はclaimを解放せず`Blocked`にする。

停止確認後にclaimを解除してもHostは`Retained`としてdataを保持する。`Adopt`は既存installation、cluster identity、desired configurationが一致する場合だけdataを保持してclaimする。`Reprovision`はdata破棄を明示承認する別lifecycleであり、Talos reset/installerへ委譲する。cleaning、reprovisioning、disk wipe、force releaseは通常updateや通常deleteの副作用にせず、別の明示的な操作、権限、監査、確認を必要とする。

`TartHost`の直接削除はforgetであり、Claim中またはRetainedのHostを`tart.cluster.x-k8s.io/forget-approved: "true"` annotationなしに削除しない。forgetが承認されてもpower off、Talos reset、disk wipeは行わず、inventoryからだけ削除する。

## MHC

すべてのTart-managed MachineでMachineHealthCheckのdelete-and-recreate remediationを安全な既定値とみなさない。初期運用では対象Machineへ`cluster.x-k8s.io/skip-remediation`を設定し、明示的なreplacement opt-inなしにMachineを削除しない。将来のexternal remediationは同じMachineとHostを維持するpower cycle/Talos recovery方式とする。MHC、rollout、手動削除の全経路でRetained gateとshutdown確認を通す。
