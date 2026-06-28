# アーキテクチャ

この文書の用語、要求レベル、時間・再試行の初期値は[記述規約と用語集](conventions.md)に従う。

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

### 1.1 責務表

| 主体 | 入力 | 実行する処理 | 永続化先 | 実行してはならない処理 |
|---|---|---|---|---|
| CAPI rollout owner | Machine/Templateの望ましいversion | Machineの作成・削除・更新順制御 | Machine、KCP、MachineDeployment | disk書き込み、BMC操作 |
| Bootstrap Provider | Machine、Cluster情報 | Bootstrap Data生成 | Kubernetes Secret | Host選択、OS image書き込み |
| TartMachine controller | TartMachine、CAPI Machine | Host割当要求、Operation作成、CAPI Condition更新 | TartMachine/TartHostOperation Status | distribution固有commandの直接実行 |
| TartHost controller | TartHost、Operation | Host phase、Driver capability、cleaningの調整 | TartHost Status | CAPI Machineのversion決定 |
| Runtime Extension | current/desired Machine群 | In-place可否判定、Operation進捗応答 | TartHostOperation | CAPIに通知せず更新開始 |
| Power/Boot Driver | Host target、Operation ID | 電源・次回boot・Virtual Media操作 | 外部BMC、結果はOperation Status | disk partition操作 |
| Provisioning Agent | 署名済みPlan | disk検出、partition、slot書き込み、検証 | 対象disk、progress API | CAPI objectの直接更新 |
| Node Lifecycle Service | 署名済みUpdate Plan | kubeadm/k3sのtyped Step実行 | State/Data、progress API | 任意shell command受付 |
| controller HTTPS server | Agent request | 認証、Plan/Bundle配信、progress受付 | Secret、Operation Status | Token/Bootstrap Dataのlog出力 |

Bootstrap Bundleはpayloadの意味をInfrastructure Providerが変更しないという意味で不透明である。ただし、`format`の識別、digest検証、対応Adapterの選択はInfrastructure Providerが行う。

## 2. デプロイ構成

管理側は単一のGoバイナリとし、次を同じcontroller-manager process内で起動する。

- TartCluster/TartMachine/TartHost/TartHostOperation reconciler
- Runtime Extension server
- ProxyDHCP、TFTP、iPXE/Agent HTTPS server
- 組み込みWoL/Redfish driver

Provisioning Agentは対象Host上の一時OSで動く別バイナリであり、管理クラスタ上のserviceではない。外部gRPC Driverを有効にした場合だけ、Driverをcontrollerとは別processとして起動する。

初期リリースのcontroller Deploymentは`replicas: 1`、`hostNetwork: true`とする。ProxyDHCP、TFTP、HTTPS listenerはleaderだけが開始する。将来`replicas`を増やす場合は、controller-runtimeのleader election取得後にlistenerを開始し、lease喪失時にlistenerを停止する。leader以外がDHCP応答またはTFTP/HTTPS listenを行う構成は禁止する。

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

文書中ではPlatform Profileと呼ぶ。初期リリースではcontroller imageに同梱するversion付き設定とし、利用者が任意内容を作成するCRDにはしない。各Profileは最低限、次を持つ。

- Profile ID。例: `amd64-uefi-ab/v1`
- CPU architectureと最低CPU level
- Firmware種別
- 使用可能なBoot Transport
- 必須Driver Capability
- Disk RoleごとのGPT type GUID、最小byte数、filesystem
- State/Dataからbind mountする絶対path一覧
- Bootstrap Adapter名と対応`format`
- `stateSchema`の現在値
- bootloader、boot trial回数、Health Gate
- Agent Artifact digest

Profileへshell commandを保存するfieldは作成しない。利用者が追加mountを指定できる場合も、sourceはData内のsubdirectory、targetは絶対path、mount typeはbind mountに限定する。

## 5. 状態機械

### 5.1 TartHost phase

| Phase | 意味 | 許可する次Phase |
|---|---|---|
| `Available` | consumerRefなし、保持Dataなし | `Reserved`、`Error` |
| `Reserved` | 1つのTartMachineへ割当済み、破壊操作前 | `Provisioning`、`Cleaning`、`Error` |
| `Provisioning` | 初期Provision Operation実行中 | `Provisioned`、`Cleaning`、`Error` |
| `Provisioned` | MachineとNodeがReady | `Updating`、`Cleaning`、`Detached`、`Error` |
| `Updating` | Update/Recovery Operation実行中 | `Provisioned`、`RecoveryRequired`、`Error` |
| `Cleaning` | State/Data消去または隔離処理中 | `Available`、`Retained`、`Detached`、`Error` |
| `Retained` | Data保持、State消去済み | `Cleaning`、`Reserved`（明示adoption時だけ） |
| `Detached` | State/Dataを保持して管理対象から外した状態 | `Reserved`（同じMachine UIDだけ）、`Cleaning` |
| `RecoveryRequired` | 自動処理を停止しSnapshot復元が必要 | `Updating`、`Cleaning` |
| `Error` | Host自体の設定またはDriverが利用不能 | 直前の安定Phase、`Cleaning` |

`Available`へ遷移する前に、`consumerRef=nil`、active Operationなし、State消去済み、保持Dataなしを検証する。

### 5.2 TartHostOperation phase

| Phase | 実行主体 | 完了条件 | 失敗時 |
|---|---|---|---|
| `Pending` | controller | Spec検証とHost lock取得 | `Failed` |
| `PreparingBoot` | Driver | 次回boot設定とPowerOn成功 | `Failed` |
| `WaitingForAgent` | controller | Agent認証とinventory受信 | `Failed` |
| `Writing` | Agent | 全target block書き込みとfsync成功 | `Failed` |
| `Verifying` | Agent | read-back digestとverity root hash一致 | `Failed` |
| `BootTrial` | Driver/Host | target slotでOS boot report受信 | `RollingBack` |
| `AwaitingHealth` | controller | PlanのHealth Gate成立 | `RollingBack`または`RecoveryRequired` |
| `DistributionUpdating` | Node Lifecycle Service | Planの全Lifecycle Step成功 | `RecoveryRequired` |
| `RollingBack` | Driver/Host | 旧slotのHealth Gate成立 | `RecoveryRequired` |
| `Succeeded` | なし | terminal | 遷移禁止 |
| `Failed` | なし | terminal | 新Operationだけ作成可能 |
| `RecoveryRequired` | operator/Recovery controller | Snapshot復元Plan作成 | `Pending`の新Operation |

controllerはPhaseを飛び越えて更新してはならない。Agent/Driverから同じ完了reportを複数受信した場合は、既に保存済みのStepとして200応答し、Phaseを再度進めない。

## 6. Driverモデル

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

Driver errorは次の4種類へ分類する。

| 種類 | Reconcile動作 |
|---|---|
| `Unsupported` | 再試行せず`Failed`。Host capability不一致をConditionへ記録 |
| `AuthenticationFailed` | 再試行せず`Error`。Secret参照名だけをMessageへ記録 |
| `Temporary` | 1秒、2秒、4秒で最大3回再試行 |
| `DeadlineExceeded` | そのStepを失敗としてOperation deadline判定へ進む |

WoL Driverは`PowerOn`だけを実装し、`PowerState`を実装しない。reachability observerがpingへ応答した場合もPowerStateは`Unknown`のままとする。

Go interfaceでWoL/Redfishを検証後、同じportをversioned protobufへ写像する。外部pluginは別processとし、controller process内へ任意WASMをロードしない。

Redfish Virtual MediaはProvisioning Agentのboot transportであり、disk writerの代替ではない。Redfish標準は任意のhost disk layoutを書き込むAPIを提供せず、Ironicのdirect deployもdeploy ramdisk上のagentがdiskへ書く。このため、BMC搭載機でも共通Agentを使用する。

## 7. ディスクとブート

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

これは論理Roleであり、全Platformへ同じpartition数や順序を強制しない。`amd64-uefi-ab/v1`の物理順序、type GUID、初期sizeはTask 01で決定してProfileへ固定する。決定前にpartition番号をAPI fieldとして公開しない。

OS artifactはpartitionへ書けるfilesystem imageであり、whole-disk raw imageではない。初期化時だけagentがpartition tableを作成し、更新時は選択したinactive OS partitionとboot metadata以外を書き換えない。

State/DataはUUIDで識別し、OSイメージ内の明示的な`.mount` unitと依存関係により、kubelet/container runtimeより前にmountする。デバイス名の列挙順序へ依存しない。必須mountへ`nofail`を付けず、失敗時にOS側の空ディレクトリへ新しいnode identityを書かせない。

## 8. OSとKubernetes Distributionの分離

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

## 9. Artifact

OCI Artifactのmanifestを正本とし、最低限次を記録する。

```yaml
schemaVersion: 1
mediaType: application/vnd.tart.os-slot.v1
os:
  family: ubuntu
  version: "24.04"
architecture: amd64
filesystem: ext4
image:
  digest: sha256:...
  sizeBytes: 8589934592
verity:
  digest: sha256:...
  rootHash: "..."
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
generation: 12
```

Manifestはcanonical JSONでserializeし、Manifest自体を署名対象とする。成果物はCIでSBOM、provenance、署名を生成する。Agentはdownload後かつblock deviceへ書く前にManifest署名とpayload digestを検証し、書き込み後に対象block deviceのSHA-256とdm-verity root hashを検証する。

### Build toolchain

Kubernetes SIGs Image BuilderはCAPI向けVM/whole-disk raw imageとKubernetes設定の生成に利用でき、Ubuntu 24.04/26.04 raw targetを持つ。一方、A/B filesystem slot、dm-verity、State/Data契約、Debian 13をそのまま満たさず、upgrade/downgrade semanticsはNon-Goalである。

Task 05で、Image Builder rawの変換、Ansible roleだけの再利用、mkosi/systemd-repart等の独自pipelineを比較する。検証前にImage Builder採用または独自実装へ固定しない。

## 10. Provisioning Agent

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

Agent APIはOperation IDとPlan Digestをidempotency keyとする。進捗は`TartHostOperation.status.completedSteps`と`agentSequence`へ保存する。`agentSequence`は1から開始する単調増加整数とし、保存済み値以下のreportは状態を変更せず200を返す。

### Initial credential

最初のsession credentialを安全にAgentへ渡す方法はplatform capabilityごとに異なる。

- 高保証profile: TPM attestation、事前登録host key、またはBMCで保護されたVirtual Mediaを使う。
- network-boot-only profile: 隔離されたprovisioning L2、HTTPS server pinning、短命でoperationへbindingしたcredentialを使う。

後者は悪意あるL2参加者に対するHost identityを提供しない。Secure Boot/TPMを持たないLegacy BIOS機ではこの制約を明記し、同等の強度を主張しない。

未決定:

- 選択肢: TPM attestation / 事前登録Host key / BMC保護media / 隔離L2用の一時credential
- 決定タスク: Task 04
- 判定基準: credentialがURL query、公開iPXE script、kernel command line、access logへ現れないこと
- 決定まで禁止する実装: Bootstrap DataをAgentへ配信するproduction endpointの有効化

### Bootstrap bundle

Bundleは`apiVersion`、`format`、`payload`、`payloadDigest`、`machineUID`、`operationUID`を持つ。MVPは`format=cloud-config`だけを受理する。OS image内のAdapterはState/Data bind mount成立後に一度だけ適用する。成功markerはpayload digest、適用時刻、Adapter versionを含むJSON fileとしてStateへrenameにより原子的に保存する。

Ignitionおよびread-only rootの未分離pathへ書く任意cloud-init customizationは、対応adapterが実証されるまでpreflightで拒否する。Bootstrap payloadを「不透明である」ことは、format検査と実行adapterが不要という意味ではない。

## 11. 初期導入のシーケンス

| # | 主体 | 入力 | 実行内容 | 成功時に保存する状態 |
|---|---|---|---|---|
| 1 | TartMachine controller | TartMachine、owner Machine | owner UID、pause、deletion、Bootstrap Secret参照を検証 | まだ変更しない |
| 2 | Host allocator | Host selector、Platform Profile | 条件に一致する`Available` Hostの`spec.consumerRef`をresourceVersion付きで更新 | Host=`Reserved`、TartMachine.hostRef |
| 3 | TartMachine controller | Artifact Manifest、Profile | digest、signature、CPU、disk最小size、stateSchemaを検証しPlan生成 | Operation=`Pending`、Plan Digest |
| 4 | Session service | Host UID、Operation UID | 256 bit token生成、hashと10分後のexpiry保存 | token Secret参照 |
| 5 | Operation controller | Operation | 必要Capabilityを照合しBoot DriverとPower Driverを呼ぶ | Operation=`PreparingBoot`から`WaitingForAgent` |
| 6 | Agent | Initial Credential | TLS接続、Host/Operation認証、disk inventory送信 | agentSequence=1 |
| 7 | Operation controller | Agent inventory | systemUUID、MAC、disk serial/WWN/sizeをTartHost指定値と照合 | inventory snapshot |
| 8 | Agent | 署名済みPlan | partition作成、State/Data初期化、OS-A/Verity-A書き込み、fsync | Operation=`Writing` |
| 9 | Agent | Manifest | block deviceのSHA-256とverity root hashを検証 | Operation=`Verifying`、Write/Verify Step |
| 10 | Agent | Bootstrap Bundle | `format`とdigestを検証しStateへrenameで配置 | Token失効、BundlePlaced Step |
| 11 | Agent/Boot Driver | OS-A | boot trial回数3でOS-Aを次回boot先に設定し再起動 | Operation=`BootTrial` |
| 12 | OS health service | Active Slot、mount結果 | slot、generation、State/Data mount、Bootstrap markerを報告 | Operation=`AwaitingHealth` |
| 13 | TartMachine controller | workload Node | Node Ready、providerID、期待versionを検証 | Machine/TartMachine Ready、Host=`Provisioned`、Operation=`Succeeded` |

Step 2より後で失敗した場合は同じOperationをStatusから再開する。Diskへ1 byteでも書き込んだ後の失敗ではHostを`Available`へ戻さず、`Cleaning`または`RecoveryRequired`へ遷移する。

## 12. インプレース更新のシーケンス

### 12.1 共通の開始処理

1. `CanUpdateMachine`/`CanUpdateMachineSet`はcurrentとdesiredのMachine、InfraMachine、BootstrapConfigをcanonical JSONへ変換して差分fieldを列挙する。
2. OSOnlyで許可する差分はOS Artifact参照とUpdate Policyだけとする。Machine versionまたはBootstrap payload digestが変わる場合はOSOnlyとして扱わない。
3. Extensionが返す3種類のpatch、またはMachineSet側の3種類のpatchで全差分を覆えない場合はin-placeを拒否する。CAPIは通常置換を選択する。
4. `UpdateMachine`はHostに非terminal Operationがなければ作成し、存在すれば同じPlan DigestのOperationを返す。異なるPlan DigestのOperationが存在する場合は`Failure`を返す。
5. Operationがterminalになるまで`status=Success`、`retryAfterSeconds=10`を返す。`Succeeded`ではretryを0、`Failed/RecoveryRequired`では`Failure`と固定Reasonを返す。

### 12.2 OSOnly

1. 現在のActive SlotとTartMachine Statusが一致することを検証する。
2. Inactive Slot以外をtargetに含むPlanを拒否する。
3. 初期導入のStep 4から9を実行する。ただしpartition table、State、Data、Bootstrap Bundleを変更しない。
4. Operationを`BootTrial`へ進め、新slotを最大3回起動する。
5. OS boot完了とNode health完了がdeadline前に成立すればCommitする。
6. 成立しなければ`RollingBack`へ進め、旧slotを起動する。
7. 旧slotのNode healthが成立すればOperation=`Failed`、成立しなければ`RecoveryRequired`とする。

### 12.3 KubernetesBinary/StateMigration

1. CAPI rollout ownerが対象Nodeを更新可能とした後だけOperationを開始する。
2. Node Lifecycle Serviceが`Preflight`を実行し、version skew、disk空き容量、State schemaを検査する。
3. control planeまたはStateMigrationでは`PrepareSnapshot`を実行し、SnapshotRefをStatusへ保存する。
4. control planeでは旧slot稼働中にtarget kubeadmの`upgrade apply`を実行する。
5. workerでは新slot起動後かつkubelet起動前に`upgrade node`を実行する。
6. `Verify`でNode Ready、期待version、control plane component、必要な場合はetcd quorumを検査する。
7. KubernetesBinaryの検証済み組合せだけRollbackを許可する。StateMigration失敗は`RecoveryRequired`とする。

現在のCAPI v1.13.1ではRuntimeSDK/InPlaceUpdatesはAlphaかつ既定無効である。通常のMachine置換とdelete-first host reuseを安定経路として維持し、同一Node更新は明示的feature gate配下に置く。

OS-only互換更新はslot rollbackの対象にできる。Kubernetes binary更新はversion skewとState互換を満たす場合だけ対象にする。`kubeadm upgrade`、etcd schema、不可逆なState migrationを伴う更新は、snapshotと明示的recovery planを別トランザクションとして用意しない限り自動rollback対象にしない。

## 13. セキュリティ境界

- 管理クラスタのKubernetes API、artifact registry、controller HTTPS endpoint、BMC networkを別の信頼境界として扱う。
- Redfish credentialはSecret参照とし、controllerのログやdriver request metadataへ値を出さない。
- gRPC pluginへKubernetes client credentialを渡さない。pluginごとに必要なcredentialだけをmountまたは外部secret providerから取得する。
- iPXE自体の完全性だけに依存しない。agentがplanと全artifactを再検証する。
- UEFI Secure Boot profileではdm-verity root hashを署名済みUKIまたは署名対象boot metadataへ固定し、OS dataとhash treeの同時改変を防ぐ。
- Secure BootなしのUEFIとLegacy BIOSではdm-verityを偶発破損検知として扱い、攻撃者によるOS/verity/boot metadataの同時改変に対する真正性を主張しない。
- tokenは平文保存せずhashだけを永続化する。Host UIDとOperation UIDへbindingし、発行から10分、認証失敗5回、Bundle配信成功のいずれかで失効させる。
- 応答結果が不明な通信切断後に同じtokenを再利用せず、controllerがoperation状態を確認して新しいsession tokenを発行する。
- 一時OSの時計が信頼できない環境を考慮し、private CA/SPKI pinningと信頼できる時刻取得の方式をplatform profileで定める。
- image URLの任意host指定を許さず、許可registryとdigest形式をvalidationする。

## 14. 可観測性

Condition typeは`Ready`、`Available`、`Provisioning`、`Updating`、`Degraded`の5種類とする。追加する場合はAPI変更ADRを作成する。ReasonはGoの定数として列挙し、Messageは英語で記録する。

メトリクスlabelは`operation_type`、`phase`、`driver`、`result`、`rollback`だけを許可する。Host名、Machine名、Operation ID、token、image URLをlabelにしない。TraceはOperation IDをattributeとしてcontroller、Driver、Agent reportを関連付ける。

## 15. 現行実装からの移行

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

## 16. 参照資料

- [記述規約と用語集](conventions.md)
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
