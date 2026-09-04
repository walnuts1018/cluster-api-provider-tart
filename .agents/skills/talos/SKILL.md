---
name: talos
description: Talos Linux API、machine configuration、storage、upgradeの委譲境界を確認する
when_to_use: Talos client、Bootstrap Config、installation、hardware discovery、OS/Kubernetes updateを実装・レビューする時
---

# Talos連携方針

## 正本

Talos APIから得られるversion、health、hardware inventory、disk、actual configurationをHost/Machineのobserved stateとして扱う。Talosが判断できる状態をTart独自のStatus phase、operation、compatibility tableへ複製しない。

## configuration

Bootstrap ProviderはTalos machineryが提供するsecrets bundle、cluster endpoint、machine role、Kubernetes version、installer image、configuration patch modelを利用する。ユーザーが指定するTalos configurationを、Tartが知っているsubsetだけへ制限しない。

disk selector、system volume、user/raw volume、encryption、system extension、kernel module、mount、kubelet設定、extra manifestなどは、Talos-native configurationまたはraw patchとして利用可能にする。Longhorn、TopoLVM、Cilium、kube-vipなどのadd-on専用fieldは作らない。

## installationとupgrade

Tartはblock deviceへ直接書き込まず、partition tableを編集せず、独自OS image formatやA/B updaterを実装しない。installationはTalos installerへ、OS upgradeとrollbackはTalos upgrade機構へ委譲する。

Talos OS version、machine configuration、Kubernetes versionは別々のdesired stateとして扱う。API call直後に完了とせず、reboot後のTalos version、reachability、health、actual configurationを観測して収束を確認する。

## client境界

controllerにはTalos generated API型を漏らさない。`talos`パッケージでclient生成、context、credential、gRPC、Close、maintenance/authenticated modeの違いを吸収し、controllerへ小さな観測型と操作interfaceだけを渡す。

Talos client errorがtransientかunsafeかを分類し、transientならbounded retry、identity mismatchや破壊的storage差分ならfail closedとする。credential、machine secret、PKI private keyはStatus、Event、log、metrics labelへ出さない。
