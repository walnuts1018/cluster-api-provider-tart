# Talos連携

TartはTalos Linux専用であり、Talosが提供するOS、storage、configuration、upgrade、rollback、etcd bootstrap、Kubernetes runtimeを再実装しない。Tartの責務はCAPIのdesired stateとTalos APIを安全に接続することである。

## Machine configurationの合成

Bootstrap ProviderはTalos machineryのversion contract、active secret bundle generation、cluster endpoint、CAPIから導出したmachine role、Kubernetes version、`TartMachine.spec.talosImage`、Secret-backed raw configuration patch modelを利用する。Talos secretsは`TartCluster.spec.id`を含むgeneration単位のimmutable bundleを参照し、BootstrapConfigごとにgenerateしない。CA rotationはTalosの段階的なCA rotation operationへ委譲し、正常完了を観測してから新generationへ切り替える。rotation中は新しいMachineのprovisioningとAdoptを開始しない。

configurationは次の順序で決定的に合成する。

```text
cluster secret bundleとCAPI/Tartのcontextからbase configurationを生成
        ↓
user-owned Talos patchを適用
        ↓
Provider-owned invariantを適用
        ↓
effective configurationをserializeし、digestを計算
```

ユーザーはTalosのraw patchを通じてTartが個別にschema化していないTalos featureも利用できる。ただし、user patchがProvider-owned fieldへ触れた場合はProviderの値で黙って上書きせず、`Ready=False`と`Reason=ConfigurationConflict`にする。

Provider-owned invariantは少なくとも次を含む。

- `TartCluster.spec.id`、Talos PKI、machine token
- cluster endpoint
- CAPI `Machine`から導出したmachine role
- CAPI `Machine.spec.version`から導出したKubernetes version-managed field
- Tartが生成したProviderID
- `TartMachine.spec.talosImage`のinstaller image identity

「Talosの全機能を使えること」と「Providerが所有するidentityを任意にoverrideできること」は別である。Provider-owned fieldをuser patchで別値へ変更できると、CAPI desired state、Node identity、cluster secret、Talos configurationが矛盾するためである。

## ProviderID bridge

ProviderIDはHost allocation後にimmutableな`TartHost.spec.id`から`tart://host/<TartHost.spec.id>`として決定し、Infrastructure ProviderとBootstrap Providerが同じ決定論的な関数で算出する。Host allocationとDiscovery bootはbootstrap dataを待たずにconsumerRef、ProviderID、inventoryを確立できるが、Talosへのconfiguration apply、install、provisioning用power操作はbootstrap dataが存在するまで開始しない。ProviderIDをKubernetes metadata UIDから分離することで、management cluster復元後も同じHost identityとProviderIDを維持する。

```text
TartMachine.spec.providerID
        ↓
Bootstrap rendererのmachine.kubelet.extraArgs.provider-id
        ↓
kubelet --provider-id=<same value>
        ↓
Node.spec.providerID
```

Node `spec.providerID`、CAPI InfraMachine `spec.providerID`、TartMachineの値は完全一致させる。不一致を観測した場合はNodeやHostを別identityとして自動修復せず、`Ready=False`と`Reason=IdentityConflict`へ反映する。ProviderIDは`TartHost.spec.id`に結び付くため、Host bindingの変更、Host IDの変更、既存Nodeへの別Host割り当ては通常updateではなくIdentity変更である。

## Talos-native configuration

ユーザー設定はTartが知っているsubsetへ制限せず、Talosのraw patchとして渡す。raw patchは全てimmutableなSecret-backed inputへ格納し、CRD Specへinline保存しない。Secretには非機密configurationを含めてもよい。次のような機能もTart専用fieldを作らず、Talos-native configurationで利用可能にする。

- system volume、user volume、raw volume
- disk selector、encryption、installer disk
- kernel parameter、kernel module、mount
- kubelet configuration、extra manifest
- kube-proxy設定などTalosが提供するKubernetes設定

複数diskのOS、EPHEMERAL、IMAGECACHE、Longhorn、TopoLVM、application local storageなどの用途は、Talosが提供するvolumeとselectorの組み合わせで表現する。EPHEMERALは永続データの保持先として扱わず、Longhorn、TopoLVM、application dataなどの永続用途はUser VolumeまたはRaw Volumeへ明示的に分離する。Linuxの`/dev/sda`や`/dev/nvme0n1`をstable identityにせず、Tart独自のpartition DSLやdisk writerを作らない。

## Talos imageとsystem extension

desired imageの正本は`TartMachine.spec.talosImage`の`{version, schematicID}`である。system extension setはImage Factoryのschematicへ組み込み、BootstrapConfigのconfiguration patchと二重に所有しない。PXE/ISOなどのboot assetとTalos installer imageには同じversionとschematicを使う。

可変tagだけに依存せず、可能な範囲でdigestなど再現可能なartifact identityも観測・検証する。schematicやextension setの変更は別Machineの作成理由ではなく、Talos OS/imageのin-place upgrade理由として扱う。Talos image取得、boot asset生成、installerへのimage指定はTalos/Image Factoryへ委譲し、独自Tart image formatを導入しない。

## Hardware discoveryとstorage

ユーザーにinstall前の`/dev/sda`、`/dev/nvme0n1`、NIC名、disk UUIDの調査を要求しない。Bootstrap SecretなしのDiscovery bootでmaintenance Talos APIからarchitecture、system UUID、NIC、address、disk model、serial、WWID、size、rotational、transport、bus情報、firmwareを取得し、`TartHost.status`へ観測として保存する。disk identityの重複を観測したら関係するHostをallocationとconfiguration applyから除外する。

disk selectionの基本identityはserial、WWID、model、transport、size、rotational、busなどstable attributeとする。Linux device pathは一時的な観測値として扱う。TartはTalosのsystem volume、user volume、raw volume、disk selector、encryption、installer disk semanticsをそのまま利用する。

## Maintenance APIのtrust model

未構成Talosのmaintenance APIはTLSで暗号化されるが認証済みではない。self-signed certificateが使われ、client certificateがなく、clientもserverも相手のidentityを検証できない。machine configurationを送った後はauthenticated Talos APIの相互TLSへ移行する。

したがってconfigurationをapplyする前に、次の情報を一つのboot attemptへ結び付ける。

```text
expected TartHost
        ↔ boot attempt
        ↔ MAC / DHCP lease / network endpoint
        ↔ observed system UUID and hardware inventory
        ↔ maintenance endpoint fingerprint when available
```

このbindingが曖昧、競合、または期待したHostと一致しない場合はconfigurationをapplyせず`Ready=False`、`Reason=IdentityConflict`にする。普通のPCでは初回に暗号学的なhardware identityを完全に証明できないため、provisioning network自体をtrusted infrastructureのsecurity boundaryとして扱うが、network trustだけでHostとendpointの論理的なbindingを省略しない。

## Installationとupgrade

Tartはblock deviceへimageを書き込まず、partition tableを編集せず、独自OS image format、A/B partition、boot trial、rollback partition managerを実装しない。Talos installerへeffective configurationを渡し、Talos upgrade APIとrollback機構へ委譲する。

Talos APIのupgrade、configuration apply、shutdown、reboot、etcd member operationの結果を即座に成功とみなさず、次のreconcileでversion、reachability、configuration digest、health、etcd membershipを再取得する。Talos APIが判断できない差分はMachine replacementへfallbackせず、`Ready=False`と安全なreasonで停止する。desired versionより古いversionへrollbackされた場合はdesired Specを戻さず、`UpdateMachine`を`Failure`、`Reason=RolledBack`として後続のControl Plane updateを停止する。

Talosの`upgrade-k8s`がlive machine configuration内のKubernetes component image versionを書き換えるため、古いfull configurationを後から再applyしてdowngradeを発生させてはならない。rendererは常にcurrent CAPI `Machine.spec.version`をversion-managed fieldへ反映し、generic user patchから分離する。

## API adapter

`talos` packageはTalos client、context、credential、gRPC option、maintenance/authenticated modeをcontrollerから隠す。controllerが必要とする観測はReachable、Architecture、SystemUUID、Version、SchematicID、Addresses、Disks、ConfigurationDigest、Healthy、ShutdownConfirmedなどの小さな型へ変換する。Schematic IDは必要に応じてTalosのvirtual `schematic` extension（`talosctl get extensions`）から観測する。

操作はApplyConfiguration、Upgrade、UpgradeKubernetes、Bootstrap、Shutdown、必要なetcd member operationに限定する。operationの送信事実をStatusへ保存せず、外部観測から未開始・実行中・完了済み・失敗を判断できるようにする。

configuration digestはraw YAML bytesではなく、Talosが解釈したeffective machine configurationを正規化したsemantic representationのSHA-256とする。field order、defaulting、serialization差分をdriftとせず、`upgrade-k8s`などが変更するversion-managed fieldはgeneric configuration driftから分離する。digestは非可逆値だけをStatusへ公開する。

configuration applyやOS upgradeがrebootを要求する場合、Update Extensionは先にNodeをquiesceする。Talos operation自身が安全なdrainを提供する場合はそれを利用し、提供しない場合はworkload cluster側でcordon/drainする。multi-node clusterではdrain成功を必須とし、失敗時はoperationを開始しない。single-node clusterではcordonとgraceful evictionを可能な範囲で試し、明示的なdowntime policyがある場合はavailabilityを理由に永久blockせず、persistent data preservationを優先してoperationを開始できる。具体的な強制drain flagをAPI contractへ固定しない。

## Bootとpower

`boot`はHost identityを受けてmaintenance environmentへ到達させる最小のbackend境界である。Wake-on-LAN、BMC/Redfish、VM API、manual、external network bootを追加できるが、PXE、DHCP、TFTP、iPXEの具体方式をTartのCRDやdomain modelへ固定しない。自動Reprovisionを提供するbackendは、installed OSからmaintenance environmentへ戻すboot strategyを持つことをcapabilityとして宣言する。Fresh machineのnetwork bootだけではReprovisionを自動許可しない。

初期boot assetはsecret-freeとする。boot authorization/correlation identityはprocess memoryやOperation CRDへ置かず、対象TartHost、consumerRef、`TartHost.spec.id`、TartMachine UID、desired image identityから決定的に再構成できるResource metadataと観測値で管理する。同じboot要求を再concileしても、対象Hostが変わったり異なるendpointへconfigurationを送ったりしない。power onの成功はTalos起動やinstallation完了を意味しないため、Talos endpoint、identity、inventory、authenticated API、healthを観測してからProvisionedへ進める。

## Kubernetes add-on

Cilium、Longhorn、TopoLVM、kube-vip、CoreDNS customization、metrics-server、ingress、observability stackをTartの専用APIへ組み込まない。必要なTalos configurationはBootstrapConfigのraw patchまたはTalos image schematicで指定し、Kubernetes Resourceの配布はClusterResourceSet、Addon Provider、GitOpsなどへ委譲する。
