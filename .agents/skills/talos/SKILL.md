---
name: talos
description: Talos Linux API、machine configuration、storage、upgradeの委譲境界を確認する
when_to_use: Talos client、Bootstrap Config、installation、hardware discovery、OS/Kubernetes updateを実装・レビューする時
---

# Talos連携方針

## 正本

Talos APIから得られるversion、schematic、health、hardware inventory、disk、actual configurationをHost/Machineのobserved stateとして扱う。Talosが判断できる状態をTart独自のStatus phase、operation、compatibility tableへ複製しない。Discovery bootはBootstrapConfigやMachine provisioningに依存せず、secret-freeなmaintenance APIからinventoryを取得する。

Talos imageのdesired identityは`TartMachine.spec.talosImage`の`{version, schematicID}`である。system extension setをBootstrapConfigと二重に所有せず、boot assetとinstaller imageには同じschematicを使う。

## configuration

Bootstrap ProviderはTalos machineryが提供するcluster secret bundle、cluster endpoint、CAPI Machineから導出したmachine role/Kubernetes version、TartMachineのimage identity、Secret-backed raw configuration patch modelを利用する。cluster secret bundleはCluster IDを含むgeneration単位でimmutable Secretを生成し、active generationを永続的な参照から選択する。CA rotationではgeneration Nを基にrotation対象のTalos/Kubernetes CAだけを更新した完全なgeneration N+1 bundleを`Pending`として先に永続化する。その後、Talos公式のaccepted CA追加、issuing CA切替、certificate refresh、旧CA削除のsemanticsをTalos machine configuration/APIでreconcileする。自動`rotate-ca`をブラックボックスとして完了後にmaterialを回収せず、Pending bundleとTalosのobserved accepted/issuing CAから再開する。generation N+1でrotation対象外のetcd CA、service account keyなどを意図せず変更しない。正常完了を観測してから新generationをactiveに確定する。rotation中は新しいMachineのprovisioningとAdoptを開始しない。BootstrapConfigごとにgenerateしない。

configurationの合成順序はbase、user-owned patch、Provider-owned invariantである。user patchは全てimmutable Secret-backed inputから読み込み、CRD Specへraw patchをinline保存しない。user patchがcluster identity、Talos PKI/token、cluster endpoint、machine role、CAPI version-managed field、ProviderID、installer image identityへ触れた場合は黙って上書きせず`Ready=False`、`Reason=ConfigurationConflict`にする。

ユーザーが指定するTalos configurationを、Tartが知っているsubsetだけへ制限しない。disk selector、system volume、user/raw volume、encryption、kernel parameter、kernel module、mount、kubelet設定、extra manifestなどはTalos-native configurationとして利用可能にする。system extension setはImage Factory schematicの所有とし、BootstrapConfigへ重複して持たせない。Longhorn、TopoLVM、Cilium、kube-vipなどのadd-on専用fieldは作らない。

ProviderIDはimmutableな`TartHost.spec.id`から`tart://host/<TartHost.spec.id>`として決定し、同じ決定論的な関数をInfrastructure ProviderとBootstrap Providerで共有する。Host allocationとDiscovery bootはbootstrap dataを待たずに開始できるが、Talos provisioningはbootstrap dataが存在するまで開始しない。effective configurationではmachine kubeletの`extraArgs.provider-id`へ注入し、Node `spec.providerID`とCAPI InfraMachineの値を一致させる。不一致は自動修復せず`Ready=False`、安全なreasonで停止する。Kubernetes objectの復元でmetadata UIDが変わっても同じProviderIDを再構築できる。

## Maintenance API

未構成Talosのmaintenance APIはTLSで暗号化されるが認証済みではない。self-signed certificate、client certificateなし、相互のidentity検証なしのtrust modelである。configuration apply前にexpected Host、boot attempt、MAC/DHCP、endpoint、observed system UUID/inventory、利用可能ならfingerprintを結び付け、曖昧ならfail closedにする。installation後はauthenticated Talos APIへ切り替える。

## installationとupgrade

Tartはblock deviceへ直接書き込まず、partition tableを編集せず、独自OS image formatやA/B updaterを実装しない。installationはTalos installerへ、OS upgradeとrollbackはTalos upgrade機構へ委譲する。

Talos OS version、machine configuration、Kubernetes versionは別々のdesired stateとして扱う。API call直後に完了とせず、reboot後のTalos version、schematic、reachability、health、actual configurationを観測して収束を確認する。desired versionより古いversionへrollbackされた場合はdesired Specを戻さず、`UpdateMachine`を`Failure`、`Reason=RolledBack`として後続のControl Plane updateを停止する。

Talosの`upgrade-k8s`がcluster-wideのKubernetes component versionを更新する場合、Control Plane Providerが一度だけ要求し、全Node actual versionを観測してからCAPI worker version propagationを進める。workerが既にdesired versionなら重複upgradeしない。

full machine configurationの再applyでは、current CAPI `Machine.spec.version`をversion-managed fieldへ反映する。古いconfigurationをそのままapplyしてKubernetes componentをdowngradeさせない。version-managed fieldをgeneric user patchから分離する。

## storage

Talosのsystem volume、user/raw volume、disk selector、encryption、installer disk semanticsをそのまま利用する。Linuxの`/dev/sda`など不安定なdevice nameを基本identityにせず、Tart独自のpartition DSLやdisk writerを作らない。install前のdisk UUID調査を要求せず、maintenance Talosからinventoryを取得してstable selectorの作成を支援する。disk identityが重複した場合は関係するHostをallocationとconfiguration applyから除外する。

## Configuration digest

configuration digestはraw YAML bytesではなく、Talosが解釈したeffective machine configurationを正規化し、secret-bearing valueをredaction markerへ置換したsemantic representationのSHA-256とする。field order、defaulting、serialization差分で不要なdriftを作らず、Talosや`upgrade-k8s`が管理するversion-managed fieldはgeneric configuration driftの比較から分離する。更新安全性はStatus digestではなくold/new Secretを解決したsemantic diffで判定し、secret値を含む内部比較結果をStatus、Event、log、metricsへ出力しない。

## Node-disruptive update

configuration applyやOS upgradeがrebootを要求する場合、Update Extensionは先にNodeをquiesceする。Talos operation自身が安全なdrainを提供する場合はそれを利用し、提供しない場合はworkload cluster側でcordon/drainを試す。drain失敗がavailability、PDB、capacityだけの理由で`TartCluster.spec.updatePolicy.allowDowntime: true`が明示されている場合はgraceful shutdown/rebootを許可し、未指定または`false`ならavailability理由でも安全停止する。destructive disk change、identity mismatch、Host mismatch、unsafe etcd membership change、quorum violationは`allowDowntime`で緩和しない。具体的な強制drain flagをAPI contractへ固定しない。

## client境界

controllerにはTalos generated API型を漏らさない。`talos`パッケージでclient生成、context、credential、gRPC、Close、maintenance/authenticated modeの違いを吸収し、controllerへ小さな観測型と操作interfaceだけを渡す。

操作は初回provisioning中のconfiguration apply、OS upgrade、Kubernetes upgrade、bootstrap、shutdown、必要なetcd member operationに限定する。初回provisioning後のmutableなOS/config updateはUpdate Extensionだけが呼び出し、通常のInfrastructure/Bootstrap reconcileはTalos observed stateを反映するだけにする。Talos client errorがtransientかunsafeかを分類し、transientならrequeue、identity mismatchや破壊的storage差分ならfail closedとする。credential、machine secret、PKI private keyはStatus、Event、log、metrics labelへ出さない。
