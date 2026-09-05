---
name: host-lifecycle
description: TartHostのallocation、identity、power、boot、data保持を確認する
when_to_use: TartHost、TartMachineのclaim、hardware discovery、power/boot backend、deletionを実装・レビューする時
---

# Host lifecycle方針

## Hostの寿命

`TartHost`はCAPI Machineより長く存続するmanagement cluster全体で一意なcluster-scoped inventoryである。Kubernetes metadata UIDとは別にimmutableな`spec.id`を持ち、concrete Resourceのnon-dry-run CREATE後にprovider controllerが一度だけ生成して永続化する。TemplateやSSA dry-runのdefaultingでrandom IDを生成せず、DR復元ではバックアップ済みの値を保持する。MAC address、system UUIDなどのstable identityは一意であることを期待する。Workload cluster側の永続identityには`TartCluster.spec.id`を使い、CAPI `Cluster.metadata.uid`へ依存しない。ただしadmission webhookの全体list検査をatomic uniquenessの根拠にせず、重複を観測した場合は関係するHostをfail closedにする。Machineの削除やscale downでHost resourceを削除せず、Host上のTalos installation、disk、local persistent dataを保持する。

## registrationとdiscovery

初期登録はMAC addressと必要なpower/boot設定から開始できるようにする。disk UUID、Linux device path、NIC名、system UUIDを事前入力必須にしない。Enrollment/DiscoveryはCAPI MachineやBootstrapConfigから独立したsecret-free maintenance bootとして実行し、maintenance Talos APIからhardware inventoryを取得してstable identityとdisk selectorをStatusへ反映する。inventory未観測でもDiscovery以外のprovisioningを開始せず、Bootstrap Secret待ちでDiscoveryを止めない。

inventoryはobserved stateであり、ユーザーが編集するdesired storage modelではない。`/dev/sda`など不安定なdevice nameを基本identityや永続的なselection keyにしない。

## claimとeligibility

明示的なHost referenceを優先し、未指定ならarchitecture、label、capability、failure domain、availabilityを満たすHostからdeterministicに選択する。`Machine.spec.failureDomain`が指定されていれば一致するHostだけを候補にする。

排他claimは`TartHost.spec.consumerRef`をcontroller-managed bindingとしてatomic CASで管理する。`GET`でresourceVersionを取得し、consumerRefがnilまたは自分のUIDであることを確認してresourceVersion付きUpdateを行う。JSON Patchの`test`も利用できる。SSAのfield ownershipを分散lockとして使わず、既存claimが別Machineを指すHostは上書きしない。`TartHost.status.claimedBy`をlockの正本にせず、Statusには`Claimed` Conditionと観測結果を置く。

Hostのallocation eligibilityは`Available`、`Claimed`、`Retained`、`Reusable`を区別する。freshなHostは`consumerRef`と`retainedFrom`がなく`Available`である。`Retained`はworkflow phaseではなく、前回のMachineのdataやTalos identityが残るため自動allocation不可である。Machine削除時はcontroller-managedな`TartHost.spec.retainedFrom`へ直前のconsumer UID、namespace、name、`TartCluster.spec.id`由来のcluster IDを記録する。`TartHost.spec.reusePolicy: Reusable`、現在の`retainedFrom`に一致する`spec.reuseApproval.retainedFromUID`、`spec.reuseMode: Adopt|Reprovision`がそろい、安全条件を再確認できた場合だけ`Reusable`にする。承認はSpecから消費せず、次の`retainedFrom.uid`が変わることで無効化する。`Adopt`にはsame cluster ID、same secret generation、same Host identity、same ProviderID、compatible role/version、expected disk identityを要求し、control-plane Adoptではetcd membershipも検証する。Claim中やfreshな時点の指定を将来の削除承認として扱わない。Cluster secret bundleが失われた後のRetained Hostは`Adopt`不可、`Reprovision`専用である。

## powerとboot

powerとbootはcapabilityとして扱い、Wake-on-LAN、Redfish、VM API、manual、external network bootを同じResource semanticsへ接続する。具体的なDHCP、TFTP、PXE方式をCRDやdomain modelへ固定しない。

power onの成功はTalos installationの成功を意味しない。maintenance endpoint、expected Hostとのidentity binding、MAC/DHCP、system UUID、inventory、authenticated Talos API、healthを観測するまでprovisionedと判定しない。初期boot assetはsecret-freeを基本とする。

maintenance Talos APIはTLSで暗号化されるが認証済みではない。Hostとendpointのbindingが曖昧ならconfigurationをapplyせず`Ready=False`と`Reason=IdentityConflict`にする。installation後はauthenticated Talos APIへ切り替える。

自動Reprovisionを許可するHostは、installed OSからremoteにmaintenance environmentへ戻せるboot strategyをcapabilityとして持つ。Fresh machineのnetwork boot capabilityだけでは自動Reprovisionを許可しない。

stable identityの重複はadmission webhookの全体list検査で排他的に防止しようとしない。同時createで検査がraceするため、controllerが重複を観測したら関係する全Hostを`Ready=False`、`Reason=IdentityConflict`としてallocationとmaintenance configuration applyを停止する。誤Hostへconfigurationを送らないことを安全性の正本とする。

## Management cluster DR

management clusterのバックアップには、`TartHost.spec.id`、stable hardware identity、`consumerRef`、`retainedFrom`、CAPI Machineとprovider resource、全secret bundle generationのSecret、power/boot backend設定を同じ整合点から含める。復元後はprovider resource、Host、CAPI Machine、cluster secretの関係を観測してから副作用を再開し、objectのmetadata UID変更を物理Host identityの変更と解釈しない。bundleが欠落または世代不明なら既存Hostへ`Adopt`せず、`Reprovision`または明示的な管理者復旧を要求する。`clusterctl move`でこの復元契約を代用しない。

## deletionと再利用

`TartMachine`削除では、CAPI Machine controllerのdrainとvolume detachが先に完了し、scale-downのcontrol planeではControl Plane Providerのpre-terminate delete hookがetcd member removalを完了していることを前提にする。その後、authenticated Talos APIへshutdown/quiesceを要求する。Hostが停止したことを確認するまでclaimとfinalizerを保持する。APIへ到達できない、停止を確認できない、またはHostが稼働し続けている場合はclaimを解放せず`Ready=False`、`Reason=ShutdownUnconfirmed`にする。

停止確認後にclaimを解除してもHostは`Retained`としてdataを保持する。`Adopt`は既存installation、same cluster ID、same secret generation、same Host identity、same ProviderID、compatible role/version、expected disk identity、desired configurationが一致する場合だけdataを保持してclaimする。control-plane Adoptではetcd membershipとNode identityも検証する。`Reprovision`はdata破棄を明示承認する別lifecycleであり、Talos reset/installerへ委譲する。cleaning、reprovisioning、disk wipe、force releaseは通常updateや通常deleteの副作用にせず、別の明示的な操作、権限、監査、確認を必要とする。

`TartHost`の直接削除はforgetであり、Claim中またはRetainedのHostを`tart.cluster.x-k8s.io/forget-approved: "true"` annotationなしに削除しない。forgetが承認されてもpower off、Talos reset、disk wipeは行わず、inventoryからだけ削除する。Tart v1alpha1では自動replacementのopt-inを提供せず、再構築は利用者によるMachineの明示的削除と`Reprovision`承認で開始する。

## MHC

すべてのTart-managed MachineでMachineHealthCheckのdelete-and-recreate remediationを安全な既定値とみなさない。初期運用ではMachineSetまたはControl PlaneのMachine templateへ生成前から`cluster.x-k8s.io/skip-remediation`を設定し、Machine作成後の後追いannotationだけに依存しない。Tart v1alpha1では自動replacementのopt-inを提供せず、再構築は利用者によるMachineの明示的削除と`Reprovision`承認で開始する。将来のexternal remediationは同じMachineとHostを維持するpower cycle/Talos recovery方式とする。MHC、rollout、手動削除の全経路でRetained gateとshutdown確認を通す。
