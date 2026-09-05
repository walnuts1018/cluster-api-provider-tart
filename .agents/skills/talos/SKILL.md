---
name: talos
description: Talos Linux API、machine configuration、storage、upgradeの委譲境界を確認する
when_to_use: Talos client、Bootstrap Config、installation、hardware discovery、OS/Kubernetes updateを実装・レビューする時
---

# Talos連携方針

## 正本

Talos APIから得られるversion、schematic、health、hardware inventory、disk、actual configurationをHost/Machineのobserved stateとして扱う。Talosが判断できる状態をTart独自のStatus phase、operation、compatibility tableへ複製しない。

Talos imageのdesired identityは`TartMachine.spec.talosImage`の`{version, schematicID}`である。system extension setをBootstrapConfigと二重に所有せず、boot assetとinstaller imageには同じschematicを使う。

## configuration

Bootstrap ProviderはTalos machineryが提供するcluster secret bundle、cluster endpoint、CAPI Machineから導出したmachine role/Kubernetes version、TartMachineのimage identity、configuration patch modelを利用する。cluster secret bundleはClusterごとに一度だけ生成されたimmutable Secretを共有し、BootstrapConfigごとにgenerateしない。

configurationの合成順序はbase、user-owned patch、Provider-owned invariantである。user patchがcluster identity、Talos PKI/token、cluster endpoint、machine role、CAPI version-managed field、ProviderID、installer image identityへ触れた場合は黙って上書きせずblockedにする。

ユーザーが指定するTalos configurationを、Tartが知っているsubsetだけへ制限しない。disk selector、system volume、user/raw volume、encryption、kernel parameter、system extension、kernel module、mount、kubelet設定、extra manifestなどはTalos-native configurationまたはImage Factory schematicとして利用可能にする。Longhorn、TopoLVM、Cilium、kube-vipなどのadd-on専用fieldは作らない。

ProviderIDは`TartMachine.spec.providerID`からmachine kubeletの`extraArgs.provider-id`へ注入し、Node `spec.providerID`とCAPI InfraMachineの値を一致させる。不一致は自動修復せずblockedにする。

## Maintenance API

未構成Talosのmaintenance APIはTLSで暗号化されるが認証済みではない。self-signed certificate、client certificateなし、相互のidentity検証なしのtrust modelである。configuration apply前にexpected Host、boot attempt、MAC/DHCP、endpoint、observed system UUID/inventory、利用可能ならfingerprintを結び付け、曖昧ならfail closedにする。installation後はauthenticated Talos APIへ切り替える。

## installationとupgrade

Tartはblock deviceへ直接書き込まず、partition tableを編集せず、独自OS image formatやA/B updaterを実装しない。installationはTalos installerへ、OS upgradeとrollbackはTalos upgrade機構へ委譲する。

Talos OS version、machine configuration、Kubernetes versionは別々のdesired stateとして扱う。API call直後に完了とせず、reboot後のTalos version、schematic、reachability、health、actual configurationを観測して収束を確認する。

Talosの`upgrade-k8s`がcluster-wideのKubernetes component versionを更新する場合、Control Plane Providerが一度だけ要求し、全Node actual versionを観測してからCAPI worker version propagationを進める。workerが既にdesired versionなら重複upgradeしない。

full machine configurationの再applyでは、current CAPI `Machine.spec.version`をversion-managed fieldへ反映する。古いconfigurationをそのままapplyしてKubernetes componentをdowngradeさせない。version-managed fieldをgeneric user patchから分離する。

## storage

Talosのsystem volume、user/raw volume、disk selector、encryption、installer disk semanticsをそのまま利用する。Linuxの`/dev/sda`など不安定なdevice nameを基本identityにせず、Tart独自のpartition DSLやdisk writerを作らない。install前のdisk UUID調査を要求せず、maintenance Talosからinventoryを取得してstable selectorの作成を支援する。

## client境界

controllerにはTalos generated API型を漏らさない。`talos`パッケージでclient生成、context、credential、gRPC、Close、maintenance/authenticated modeの違いを吸収し、controllerへ小さな観測型と操作interfaceだけを渡す。

操作はconfiguration apply、OS upgrade、Kubernetes upgrade、bootstrap、shutdown、必要なetcd member operationに限定する。Talos client errorがtransientかunsafeかを分類し、transientならrequeue、identity mismatchや破壊的storage差分ならfail closedとする。credential、machine secret、PKI private keyはStatus、Event、log、metrics labelへ出さない。
