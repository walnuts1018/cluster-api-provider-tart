# Talos連携

TartはTalos Linux専用であり、Talosが提供するOS、storage、configuration、upgrade、rollback、node managementを再実装しない。Tartの責務はCAPIのdesired stateとTalosのAPIを安全に接続することである。

## Machine configuration

Bootstrap ProviderはTalos machineryのversion contract、secrets bundle、cluster endpoint、machine role、Kubernetes version、installer image、raw configuration patch modelを利用する。cluster Secretはcluster単位で一度生成し、control planeとworkerのmachine configurationで再利用する。

ユーザー設定はTartが知っているsubsetへ制限せず、Talosのraw patchとして渡す。次のような機能もTart専用fieldを作らず、Talos-native configurationで利用可能にする。

- system volume、user volume、raw volume
- disk selector、encryption、installer disk
- system extension、kernel module、mount
- kubelet configuration、extra manifest
- kube-proxy設定などのTalosが提供するKubernetes設定

configuration生成後のpatch適用、serialization、digest計算は決定的に行う。Secretの内容やprivate keyはStatus、log、Event、metricsへ渡さない。

## Hardware discoveryとstorage

ユーザーにinstall前の`/dev/sda`、`/dev/nvme0n1`、NIC名、disk UUIDの調査を要求しない。maintenance Talos APIからarchitecture、system UUID、NIC、address、disk model、serial、WWID、size、rotational、transport、bus情報、firmwareを取得し、`TartHost.status`へ観測として保存する。

disk selectionの基本identityはserial、WWID、model、transport、size、rotational、busなどstable attributeとする。Linux device pathは一時的な観測値として扱い、Tart独自のpartition DSLやdisk writerを作らない。

複数diskのOS、EPHEMERAL、IMAGECACHE、Longhorn、TopoLVM、application local storageなどの用途は、Talosが提供するvolumeとselectorの組み合わせで表現する。Tartはその意味を専用domain modelへ取り込まない。

## Installationとupgrade

Tartはblock deviceへimageを書き込まず、partition tableを編集せず、独自OS image format、A/B partition、boot trial、rollback partition managerを実装しない。Talos installerへconfigurationを渡し、Talos upgrade APIとrollback機構へ委譲する。

Talos imageのdesired identityは可変tagだけに依存せず、可能な範囲でdigestなど再現可能なartifact identityを利用する。image変更は新しいMachineを作る理由ではなく、既存MachineへTalos upgradeを適用する理由である。

## API adapter

`talos` packageはTalos client、context、credential、gRPC option、maintenance/authenticated modeをcontrollerから隠す。controllerが必要とする観測はReachable、Architecture、SystemUUID、Version、Addresses、Disks、ConfigurationDigest、Healthyなどの小さな型へ変換する。

操作はApplyConfiguration、Upgrade、Bootstrap、必要なetcd member operationに限定する。操作結果を即座に成功とみなさず、次のreconcileで外部状態を観測する。Talos APIが判断できない差分はMachine replacementへfallbackせず、安全停止する。

## Bootとpower

`boot`はHost identityを受けてmaintenance environmentへ到達させる最小のbackend境界である。Wake-on-LAN、BMC/Redfish、VM API、manual、external network bootを追加できるが、PXE、DHCP、TFTP、iPXEの具体方式をTartのCRDやdomain modelへ固定しない。

初期boot assetはsecret-freeとする。power onの成功はTalos起動やinstallation完了を意味しないため、Talos endpoint、identity、inventory、authenticated API、healthを観測してからProvisionedへ進める。

## Kubernetes add-on

Cilium、Longhorn、TopoLVM、kube-vip、CoreDNS customization、metrics-server、ingress、observability stackをTartの専用APIへ組み込まない。必要なTalos configurationはBootstrap Configのraw patchで指定し、Kubernetes Resourceの配布はClusterResourceSet、Addon Provider、GitOpsなどへ委譲する。
