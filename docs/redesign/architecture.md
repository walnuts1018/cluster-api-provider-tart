# アーキテクチャ

## 1. 境界と責務

```text
CAPI controllers / Bootstrap Provider
                 |
                 v
        TartMachine Reconciler <---- Runtime Extension
                 |
        application use cases
      /          |             \
 host allocator  operation      secure delivery
      |          orchestrator          |
      v               |                v
   TartHost       driver ports     HTTPS endpoints
                      |
       +--------------+----------------+
       |              |                |
    WoL/iPXE       Redfish        future gRPC
       |              |                |
       +--------------+----------------+
                      |
         ephemeral provisioning agent
                      |
       disk layout / slot / boot control
```

Infrastructure Providerは「物理ホスト、OS配置、起動可能性」を担当する。クラスタ初期化内容はBootstrap Providerが所有し、本Providerは不透明なbundleとして配送・配置・実行する。

## 2. デプロイ構成

管理側は引き続き単一のGoバイナリを基本とし、次を同じcontroller-manager process内で起動する。

- TartCluster/TartMachine/TartHost/TartHostOperation reconciler
- Runtime Extension server
- ProxyDHCP、TFTP、iPXE/Agent HTTPS server
- 組み込みWoL/Redfish driver

Provisioning Agentは対象host上の一時OSで動く別バイナリであり、管理側microserviceではない。将来のgRPC driverだけは障害分離とvendor依存のため別processを許可する。

`hostNetwork`でwell-known portを使用するnetwork serverは同時に1つだけactiveにする。初期リリースはreplica 1を明示し、HA化する場合はcontroller-runtimeのleader electionとnetwork serverのlisten開始・停止を連動させる。

## 3. レイヤー

### API

Kubebuilderで生成するCRD、defaulting、validation、conversionを配置する。API型にdriver実装や状態遷移ロジックを持たせない。

### Controller

Kubernetesオブジェクトの取得、pause/deletionの処理、owner確認、patch、Condition/Event更新だけを担当する。割当、更新可否、状態遷移、再試行方針をcontroller関数へ直接書かない。

### Application

次のuse caseを小さなinterfaceに依存して実装する。

- host allocation/release
- initial provisioning
- in-place update
- deprovision/cleaning
- provisioning session発行
- operation recovery

### Domain

副作用のない値と状態遷移を保持する。

- Host allocation state
- Operation phaseと許可される遷移
- Disk/slot plan
- Artifact identityと互換性
- Driver capability
- Token digestと期限

### Port / Adapter

Power、Boot、Media、Artifact、Kubernetes persistence、HTTP delivery、Distribution Lifecycleをportとして分離し、WoL、Redfish、Kubernetes client、kubeadm等をadapterとして実装する。

## 4. リソースモデル

### TartHost

物理資産の長寿命インベントリであり、CAPI Machineより長く存続する。

```yaml
spec:
  identifiers:
    systemUUID: "..."
    bootMACAddress: "..."
  architecture: amd64
  firmware: UEFI
  rootDeviceHints:
    deviceName: /dev/disk/by-id/...
    serialNumber: "..."
    minSizeBytes: 256000000000
  management:
    powerDriver: wol
    bootDriver: ipxe
    credentialsSecretRef: null
  capabilities:
    - PowerOn
    - NetworkBoot
  consumerRef: {}
status:
  phase: Available
  powerState: Unknown
  inventory: {}
  conditions: []
```

`spec.consumerRef`はcontrollerが楽観ロックで設定する望ましい割当である。`spec.capabilities`は利用者の要求または登録情報であり、実際にdriverが報告した能力はStatusへ分離する。Secret値はCRへ埋め込まない。

### TartMachine / TartMachineTemplate

CAPI InfraMachine契約を担い、Machineの世代ごとの望ましいOS状態を表現する。

```yaml
spec:
  providerID: "tart://..."
  image:
    ref: oci://registry.example/slot-image@sha256:...
  provisioningProfile: ubuntu-ab-v1
  updatePolicy:
    mode: InPlace
status:
  hostRef: {}
  initialization:
    provisioned: true
  operationRef: {}
  activeSlot: A
  installedImageDigest: "sha256:..."
  conditions: []
```

Templateから複製されるフィールドと、割当結果・進行状態を明確に分ける。URLではなくdigest付きartifact参照を正本とする。

### TartHostOperation

初期導入、更新、rollback、cleaningの進行を再起動後も再開するための短寿命リソースである。

```yaml
spec:
  operationID: "..."
  type: Update
  hostRef: {}
  machineRef: {}
  planDigest: "sha256:..."
  desiredObjectsDigest: "sha256:..."
  targetImageDigest: "sha256:..."
  targetDistributionVersion: v1.35.0
  targetSlot: B
status:
  phase: DistributionUpdating
  lifecyclePhase: Apply
  snapshotRef: {}
  completedSteps:
    - Preflight
    - Snapshot
  attempt: 1
  agentSequence: 8
  deadline: "..."
  conditions: []
```

1 hostにつきactive operationは1つだけ許可する。`TartMachine`と`TartHost`には参照と要約Conditionだけを持たせ、長時間処理の詳細を重複保存しない。

### ProvisioningProfile

初期段階ではcontroller設定としてversion管理し、意味論が安定した段階でCRD化を判断する。最低限、次を含む。

- partition tableと各partitionの最小サイズ
- boot方式
- OSスロットのfilesystem type
- State/Dataからbind mountするパス
- Bootstrap実行unit
- state schema version

利用者が自由なshellやmount unitを注入する汎用実行機構にはしない。

## 5. Driverモデル

要求された4メソッドを1つのinterfaceへまとめると、WoLのように状態取得や電源OFFができない実装が常時エラーを返す。能力別に分割する。

```go
type PowerOnDriver interface {
    PowerOn(context.Context, HostTarget, OperationID) error
}

type PowerControlDriver interface {
    PowerOnDriver
    PowerOff(context.Context, HostTarget, OperationID) error
    PowerState(context.Context, HostTarget) (PowerState, error)
}

type BootOverrideDriver interface {
    SetNextBoot(context.Context, HostTarget, BootTarget, OperationID) error
}

type VirtualMediaDriver interface {
    Mount(context.Context, HostTarget, Artifact, OperationID) error
    Unmount(context.Context, HostTarget, OperationID) error
}
```

- `OperationID`は重複要求をdriver側で冪等化する鍵である。
- `PowerState`は`On`、`Off`、`Unknown`を持つ。
- `BootTarget`は文字列ではなく閉じた型とし、未対応能力は呼び出す前に判定する。
- controllerはdriver固有のBMC URIやSwitchBot APIを解釈しない。

Go interfaceでWoL/Redfishを検証後、同じportをversioned protobufへ写像する。外部pluginは別processとし、controller process内へ任意WASMをロードしない。

Redfish Virtual MediaはProvisioning Agentのboot transportであり、disk writerの代替ではない。Redfish標準は任意のhost disk layoutを書き込むAPIを提供せず、Ironicのdirect deployもdeploy ramdisk上のagentがdiskへ書く。このため、BMC搭載機でも共通Agentを使用する。

## 6. ディスクとブート

### 標準UEFIプロファイル

| 論理role | 用途 | 通常時 |
|---|---|---|---|
| Boot/ESP | bootloader、slot entry、更新状態 | RWを最小化 |
| OS-A | root filesystem image | dm-verity経由のRO |
| Verity-A | OS-Aのhash tree | RO |
| OS-B | root filesystem image | dm-verity経由のRO |
| Verity-B | OS-Bのhash tree | RO |
| State | node identity、PKI、cluster state | RW |
| Data | runtime、PV data | RW |

これは論理roleであり、全platformへ同じpartition数や順序を強制しない。Legacy BIOSでは小さなBIOS boot partitionを追加する。Raspberry Piではfirmware partitionとブートフローが異なるため、別profileとする。

OS artifactはpartitionへ書けるfilesystem imageであり、whole-disk raw imageではない。初期化時だけagentがpartition tableを作成し、更新時は選択したinactive OS partitionとboot metadata以外を書き換えない。

State/DataはUUIDで識別し、OSイメージ内の明示的な`.mount` unitと依存関係により、kubelet/container runtimeより前にmountする。デバイス名の列挙順序へ依存しない。必須mountへ`nofail`を付けず、失敗時にOS側の空ディレクトリへ新しいnode identityを書かせない。

## 7. OSとKubernetes Distributionの分離

OS profileはdisk layout、mount、kernel、initramfs、base userspaceを所有する。Kubernetes distribution profileは必要なbinary、service unit、永続path、health check、対応Bootstrap/Control Plane Providerを所有する。成果物pipelineで両者を組み合わせるが、APIとapplication serviceでは別の互換軸として扱う。

- kubeadm: CABPKがBootstrap Dataを生成し、KCPまたはMachineDeploymentがversionと更新順を所有する。
- k3s: 対応するBootstrap/Control Plane Providerを選定または別コンポーネントとして実装する。

Infrastructure ProviderはBootstrap Dataを不透明なpayloadとして運び、`kubeadm init/join`やk3s tokenの内容をcontroller内で組み立てない。

KCP/CABPKは既存node上で`kubeadm upgrade`を実行しないため、Kubernetes更新には別の`DistributionLifecycleDriver`が必要である。これは任意shellを実行するremote APIではなく、versionedなtyped operationだけを受け付ける。

```go
type DistributionLifecycleDriver interface {
    Preflight(context.Context, UpdatePlan) (PreflightResult, error)
    PrepareSnapshot(context.Context, UpdatePlan) (SnapshotRef, error)
    Apply(context.Context, UpdatePlan) error
    Verify(context.Context, UpdatePlan) (HealthResult, error)
}
```

kubeadm adapterは新OS slot内の署名済みone-shot node serviceから、対応versionに限定して`kubeadm upgrade plan/apply/node`を実行する。CAPI rollout owner（control planeはKCP、workerはMachineDeployment）がversionとnode順序を所有し、adapterがlocal state変更、snapshot、health確認を所有する。k3sは対応するBootstrap/Control Plane Providerと専用adapterの契約を別途定義する。

workerはcontrol planeがtarget versionを受理した後、inactive slotをstageする。新slotではkubeletを開始する前にState/Dataをmountし、`kubeadm upgrade node`を実行する。control planeは旧slot稼働中にpreflightとsnapshotを完了し、target kubeadmで`upgrade apply`を実行した後に新slotを試行起動する。各stepの前後でplan digest、target version、snapshotRef、完了markerを`TartHostOperation`へ保存する。

初期リリースのA/B更新はOS-onlyに限定する。Distribution Lifecycleが完成するまで、Kubernetes version差分を`CanUpdate*`で覆ってはならない。

## 8. Artifact

OCI Artifactのmanifestを正本とし、最低限次を記録する。

```yaml
schemaVersion: 1
os:
  family: ubuntu
  version: "24.04"
architecture: amd64
filesystem: ext4
image:
  digest: sha256:...
  sizeBytes: 8589934592
stateSchema:
  min: 1
  max: 1
kubernetes:
  distribution: kubeadm
  version: v1.35.0
boot:
  kernelDigest: sha256:...
  initrdDigest: sha256:...
requirements:
  cpuLevel: x86-64-v1
verity:
  rootHash: "..."
generation: 12
```

成果物はCIでSBOM、provenance、署名を生成する。agentはdownload後かつblock deviceへ書く前にdigestと署名ポリシーを検証し、書き込み後にread-backとdm-verity root hashを検証する。

### Build toolchain

Kubernetes SIGs Image BuilderはCAPI向けVM/whole-disk raw imageとKubernetes設定の生成に利用でき、Ubuntu 24.04/26.04 raw targetを持つ。一方、A/B filesystem slot、dm-verity、State/Data契約、Debian 13をそのまま満たさず、upgrade/downgrade semanticsはNon-Goalである。

Task 05で、Image Builder rawの変換、Ansible roleだけの再利用、mkosi/systemd-repart等の独自pipelineを比較する。検証前にImage Builder採用または独自実装へ固定しない。

## 9. Provisioning Agent

Agentは一時OS内で実行し、controllerからhostへ入るSSHを必要としない。

1. iPXE/Virtual Mediaでkernel、initramfs、短命session bootstrapを取得する。
2. agentが外向きTLS接続でcontrollerへ登録する。
3. controllerはtoken、host identity、operation IDを照合し、署名済みplanを返す。
4. agentはinventoryを報告し、root diskを複数条件で特定する。
5. 初期導入ならpartitionを作成し、更新ならinactive slotだけを選ぶ。
6. artifactを検証して書き込み、read-back検証を行う。
7. Bootstrap bundleを1レスポンスで取得してStateへ配置し、取得成功時にtokenを消費する。
8. bootloaderへ「次回のみ対象slot、失敗可能回数付き」を設定して再起動する。
9. 起動後のhealth confirmation unitがcontrollerへslot、image digest、node identityを報告する。
10. controllerがNode Ready等を確認してslotを確定する。期限内に確認できなければ旧slotへ戻す。

Agent APIはoperationごとに冪等で、進捗はcontroller memoryではなくCR Statusへ保存する。

### Initial credential

最初のsession credentialを安全にAgentへ渡す方法はplatform capabilityごとに異なる。

- 高保証profile: TPM attestation、事前登録host key、またはBMCで保護されたVirtual Mediaを使う。
- network-boot-only profile: 隔離されたprovisioning L2、HTTPS server pinning、短命でoperationへbindingしたcredentialを使う。

後者は悪意あるL2参加者に対するhardware identityを提供しない。Secure Boot/TPMを持たないLegacy BIOS機ではこの制約を明記し、同等の強度を主張しない。credentialを公開iPXE script、URL query、ログへ置かない具体方式はTask 04のspikeで確定する。

### Bootstrap bundle

bundleは少なくとも`format`、payload digest、Machine UID、operation IDを持つ。MVPはCABPKの標準`cloud-config`だけを対象とし、OS image内のadapterがState/Data bind mount成立後に一度だけ適用する。成功markerはpayload digestとともにStateへ原子的に保存する。

Ignitionおよびread-only rootの未分離pathへ書く任意cloud-init customizationは、対応adapterが実証されるまでpreflightで拒否する。Bootstrap payloadを「不透明である」ことは、format検査と実行adapterが不要という意味ではない。

## 10. 初期導入のシーケンス

```text
Machine -> TartMachine controller: reconcile
controller -> TartHost: atomic reserve
controller -> Bootstrap Secret: read
controller -> session store: token hash + expiry
controller -> power/boot drivers: network boot
agent -> controller: register(operationID, token, inventory)
controller -> agent: signed plan
agent -> artifact registry: fetch and verify
agent -> disk: partition + write OS-A + initialize State/Data
agent -> controller: fetch one-shot bootstrap bundle
agent -> bootloader: boot OS-A once
OS-A -> controller: confirm boot
controller -> CAPI: initialization.provisioned=true
```

## 11. インプレース更新のシーケンス

1. `CanUpdateMachine`/`CanUpdateMachineSet`がimage、Machine version、BootstrapConfig、各Template、互換性を検査する。
2. `machinePatch`、`infrastructureMachinePatch`、`bootstrapConfigPatch`とMachineSet側の3 patchについて、安全に処理可能な差分だけをcurrent objectへ適用する。1つでも未対応差分が残る場合は通常置換へフォールバックする。
3. `UpdateMachine`は`TartHostOperation`を作成または再開し、完了までは`retryAfterSeconds`を返す。
4. workload drain、etcd quorum、PDB等の調整はCAPI/KCP側のhook契約に従う。Infrastructure Providerが独自に全ノードを同時更新しない。
5. hostを一時provisioning環境へ起動し、inactive slotへ書く。
6. State schema互換性、Bootstrap差分、Kubernetesの対応可能なupgrade pathを検証する。
7. OS-only更新では新slotを試行起動し、Node Ready後にslotを確定する。
8. Kubernetes更新ではDistributionLifecycleDriverがpreflight、snapshot、apply、health確認を行う。
9. 成功ならactive slotとdistribution generationを確定し、失敗なら更新classに応じたrollback/recovery Conditionを設定する。

現在のCAPI v1.13.1ではRuntimeSDK/InPlaceUpdatesはAlphaかつ既定無効である。通常のMachine置換とdelete-first host reuseを安定経路として維持し、同一Node更新は明示的feature gate配下に置く。

OS-only互換更新はslot rollbackの対象にできる。Kubernetes binary更新はversion skewとState互換を満たす場合だけ対象にする。`kubeadm upgrade`、etcd schema、不可逆なState migrationを伴う更新は、snapshotと明示的recovery planを別トランザクションとして用意しない限り自動rollback対象にしない。

## 12. セキュリティ境界

- 管理クラスタのKubernetes API、artifact registry、controller HTTPS endpoint、BMC networkを別の信頼境界として扱う。
- Redfish credentialはSecret参照とし、controllerのログやdriver request metadataへ値を出さない。
- gRPC pluginへKubernetes client credentialを渡さない。pluginごとに必要なcredentialだけをmountまたは外部secret providerから取得する。
- iPXE自体の完全性だけに依存しない。agentがplanと全artifactを再検証する。
- UEFI Secure Boot profileではdm-verity root hashを署名済みUKIまたは署名対象boot metadataへ固定し、OS dataとhash treeの同時改変を防ぐ。
- Secure BootなしのUEFIとLegacy BIOSではdm-verityを偶発破損検知として扱い、攻撃者によるOS/verity/boot metadataの同時改変に対する真正性を主張しない。
- tokenは平文保存せずhashだけを永続化する。時刻制限、operation/hostへのbinding、単回消費、一定回数の失敗で失効を実装する。
- 応答結果が不明な通信切断後に同じtokenを再利用せず、controllerがoperation状態を確認して新しいsession tokenを発行する。
- 一時OSの時計が信頼できない環境を考慮し、private CA/SPKI pinningと信頼できる時刻取得の方式をplatform profileで定める。
- image URLの任意host指定を許さず、許可registryとdigest形式をvalidationする。

## 13. 可観測性

Condition typeは状態名の乱立を避け、少なくとも`Ready`、`Available`、`Provisioning`、`Updating`、`Degraded`を一貫して使用する。ReasonとMessageは英語で記録する。

メトリクスはoperation latency、phase、driver、result、rollbackを低カーディナリティで持つ。host名、token、image URLをlabelにしない。traceはoperation IDでcontroller、driver、agent reportを関連付ける。

## 14. 現行実装からの移行

再利用候補:

- TartHost/TartMachineの基本CRDと割当の排他制御
- ProxyDHCP、TFTP、動的iPXE HTTP
- one-time tokenのドメイン型と短命配信
- application/domain/adapterのレイヤー
- Runtime Extension serverの骨格
- OpenTelemetry基盤

置換対象:

- `image`をboot kernel/ISO URLとして扱うモデル
- WoLを直接依存するprovisioning service
- token取得をもってprovisioning完了とする状態遷移
- whole-disk raw imageを更新単位にするビルド
- `SetBootDevice(string)`のような能力を表現できない単一driver

移行は破壊的API変更を一度に行わない。新しい型をKubebuilderで追加し、conversion/defaultingと互換期間を設け、保存バージョンの移行後に旧フィールドを削除する。

## 15. 参照資料

- [Cluster API Runtime SDK In-Place Update Hooks](https://main.cluster-api.sigs.k8s.io/tasks/experimental-features/runtime-sdk/implement-in-place-update-hooks)
- [Cluster API InfraMachine contract](https://main.cluster-api.sigs.k8s.io/developer/providers/contracts/infra-machine)
- [Metal3 provisioning](https://book.metal3.io/bmo/provisioning.html)
- [systemd.mount](https://www.freedesktop.org/software/systemd/man/latest/systemd.mount.html)
- [Linux dm-verity](https://docs.kernel.org/admin-guide/device-mapper/verity.html)
- [Redfish specification](https://www.dmtf.org/standards/redfish)
- [Kubernetes SIGs Image Builder](https://github.com/kubernetes-sigs/image-builder)
- [Image Builder raw provider](https://image-builder.sigs.k8s.io/capi/providers/raw)
- [Ironic Redfish driver](https://docs.openstack.org/ironic/latest/admin/drivers/redfish.html)
- [Ironic deploy interfaces](https://docs.openstack.org/ironic/latest/admin/interfaces/deploy.html)
