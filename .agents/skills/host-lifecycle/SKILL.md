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

## claim

明示的なHost referenceを優先し、未指定ならarchitecture、label、capability、availabilityを満たすHostからdeterministicに選択する。既存claimが別Machineを指すHostは上書きせず、競合として扱う。

Host identity、Machine identity、claimを通常のimage/config updateで変更しない。identity mismatchを検出した場合はTalos APIやpower backendへ副作用を送らず、blocked Conditionへ反映する。

## powerとboot

powerとbootはcapabilityとして扱い、Wake-on-LAN、Redfish、VM API、manual、external network bootを同じResource semanticsへ接続する。具体的なDHCP、TFTP、PXE方式をCRDやdomain modelへ固定しない。

power onの成功はTalos installationの成功を意味しない。maintenance endpoint、identity、inventory、authenticated Talos API、healthを観測するまでprovisionedと判定しない。初期boot assetはsecret-freeを基本とする。

## deletionと再利用

`TartMachine`削除ではHost claimだけを解除し、cleaning、reprovisioning、disk wipeを実行しない。既存dataが残ったHostを再利用する処理は、通常更新とは別の明示的な操作、権限、監査、確認を必要とする。
