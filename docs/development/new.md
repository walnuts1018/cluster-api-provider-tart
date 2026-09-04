# Tart 新アーキテクチャ設計書

## 1. 概要

Tart は、Talos Linux を実行する物理マシンを Cluster API から宣言的に管理するための Cluster API Provider である。

Tart は以下の3種類の Provider をすべて提供する。

* Infrastructure Provider
* Bootstrap Provider
* Control Plane Provider

既存の Talos Bootstrap Provider / Control Plane Provider には依存しない。

本プロジェクトは Talos Linux 専用とする。汎用 OS provisioning framework を目指さず、Talos が提供する API、machine configuration、installer、upgrade、disk management、bootstrap 機構を最大限そのまま利用する。

最も重要な設計目標は、一般的な Cluster API Provider で採用される「Machine は使い捨て」という前提だけに依存せず、物理マシン上に永続データを持つ環境でも安全に利用できることである。

Talos Linux、Kubernetes、machine configuration の通常の更新では Machine を置換せず、既存 Machine に対する in-place update を第一選択とする。

---

# 2. 設計目標

Tart は次の要件を満たすこと。

### 完全自動 provisioning

OS が存在しないマシンについて、ユーザーが事前に必要最小限の Host 情報を登録するだけで、

```text
Host discovery
→ Talos 起動
→ hardware discovery
→ Talos install
→ machine configuration 適用
→ Kubernetes control plane bootstrap / worker join
→ Cluster Ready
```

までを自動化できること。

### Talos-native

Talos が既に持っている機能を Tart 独自機能として再実装しない。

特に以下は原則として Talos に委譲する。

* OS installation
* machine configuration
* disk / volume provisioning
* system extension
* Talos API
* Talos OS upgrade
* rollback
* Kubernetes component management
* etcd bootstrap
* hardware information取得

### 永続 Machine

Talos/Kubernetes の更新や machine configuration の変更を理由として、物理 Machine を安易に削除・再作成しない。

特に Longhorn、TopoLVM、local PV その他のローカル永続データが存在する可能性を常に考慮する。

### Cluster API native

独自の lifecycle orchestration framework を作るのではなく、Cluster API の Provider contract、Conditions、Runtime Extensions、in-place update、upgrade orchestration などを利用する。

### 柔軟性

Cilium、Longhorn、TopoLVM、kube-vip など特定の Kubernetes ソフトウェアを Tart の API に組み込まない。

Talos machine configuration と Kubernetes の標準的な add-on 配布方式を通じて利用可能にする。

### 小さな責務

Tart 独自の概念をできるだけ増やさない。

特に Workflow engine、Operation CRD、Provisioning Plan、独自 Agent protocol などを導入しない。

---

# 3. 非目標

Tart は以下を目的としない。

* 汎用 Linux installer
* Ubuntu / Debian / Fedora 等への対応
* 独自 initramfs ベース Provisioning Agent
* 独自 disk partition engine
* 独自 OS updater
* 独自 Kubernetes distribution
* Kubernetes add-on manager
* Longhorn 専用 Provider
* TopoLVM 専用 Provider
* Cilium 専用 Provider
* kube-vip 専用 Provider
* DHCP/TFTP/PXE implementation 自体をプロジェクトの中心機能とすること
* ハードウェア固有の RAID/BMC 管理システムを包括的に提供すること

必要な外部機能との統合は行ってよいが、それらを Tart の domain model の中心にしてはならない。

---

# 4. 対象プラットフォーム

初期実装の主要対象は物理 x86-64 マシンとする。

対象には以下を含む。

* 一般的な自作 PC
* ミニ PC
* Workstation
* Wake-on-LAN のみ利用できる PC
* BMC を搭載したラックサーバー
* Redfish 等による限定的な power control が可能なサーバー

BMC virtual media は要求しない。

将来的に以下へ拡張可能な設計とする。

* Proxmox VM
* その他 VM
* Raspberry Pi 4 など Talos が対応する ARM64 machine

Host lifecycle のモデルは「bare metal x86 固有」にならないようにする。

ただし、すべての platform を一つの巨大な抽象化で事前に一般化してはならない。現在必要な abstraction のみを作り、将来の backend を追加可能な責務境界を維持する。

LXC のように Talos 自身の kernel を boot しない環境は通常の Talos Machine と同一視せず、将来必要になった時点で実現可能性を別途判断する。

---

# 5. 全体アーキテクチャ

概念上、Tart は次の構造を持つ。

```text
                         Cluster API
                              |
          +-------------------+-------------------+
          |                   |                   |
          v                   v                   v
 Infrastructure Provider  Bootstrap Provider  Control Plane Provider
          |                   |                   |
          +-------------------+-------------------+
                              |
                         Tart Machine
                              |
                    +---------+---------+
                    |                   |
                    v                   v
              Host lifecycle       Talos API
                    |                   |
              power / boot       configuration
                    |             install/update
                    +---------+---------+
                              |
                          Talos Linux
                              |
                         Kubernetes
```

責務境界として、

```text
Cluster API
    desired cluster topology / rollout

Tart
    host allocation
    physical/virtual machine lifecycle
    Talos configuration delivery
    Talos lifecycle integration

Talos
    OS
    disk
    configuration
    upgrade
    Kubernetes runtime

Kubernetes addon layer
    CNI
    CSI
    kube-vip
    observability
    その他 addon
```

を維持する。

---

# 6. 基本設計原則

## 6.1 Desired State を正本とする

Controller 内部の処理状態を正本にしない。

Reconcile は常に、

```text
Kubernetes 上の desired state
+
TartHost の observed state
+
Talos API から得られる observed state
+
必要に応じて workload cluster の observed state
```

から次に必要な処理を判断できなければならない。

Controller 再起動によって provisioning や update の意味が失われてはならない。

---

## 6.2 長時間処理用 Operation CRD を作らない

旧 Tart の `TartHostOperation` に相当する Resource は作らない。

以下のような内部 state machine を Kubernetes Resource として永続化しない。

```text
Pending
PreparingBoot
WaitingForAgent
Writing
Verifying
BootTrial
AwaitingHealth
...
```

処理の進捗は Talos や現実の Machine の状態を観測して判断する。

Kubernetes Status には observed state と Condition を保存してよいが、それを独自 workflow engine の program counter として使用してはならない。

---

## 6.3 Talos を Node Lifecycle Agent として扱う

独自 Provisioning Agent は作らない。

初期起動時の Talos maintenance environment と、インストール後の authenticated Talos API を Tart の node-management interface として利用する。

したがって Tart は、OS 内に独自 daemon を常駐させることを要求しない。

---

## 6.4 Talos の設定を再抽象化しない

Talos が表現できる設定について、Tart 独自 DSL を作らない。

例えば、

```text
LonghornDisk
TopoLVMDisk
CiliumMode
KubeVIPMode
```

のような Tart 固有概念を作成しない。

ユーザーが Talos machine configuration の能力を直接利用できるようにする。

Tart は必要な Cluster 情報、Machine identity、証明書、endpoint 等を生成し、それとユーザー指定の Talos configuration を安全に合成する。

---

# 7. Resource Model

必要な主要 Resource は次の通りとする。

## TartHost

長寿命の physical/virtual host inventory を表す。

Cluster API Machine より寿命が長い。

Machine が削除されても TartHost 自体は原則として削除されない。

TartHost が持つ責務は、

* Host identity
* boot に必要な identity
* power-management configuration
* hardware inventory
* network identity
* current allocation
* accessibility / readiness
* provisioning に必要な capability

である。

hardware inventory は observed state であり Status に保持する。

例として、

* architecture
* system UUID
* NIC
* MAC address
* IP address
* disk
* disk model
* serial
* WWID 等の stable identity
* disk size
* rotational/non-rotational
* transport
* firmware/boot information

などを表現できること。

具体的にどの情報を CRD に保持するかは実装時に必要性を判断する。

---

## TartCluster

Cluster API Infrastructure Cluster contract を実装する。

TartCluster は個々の Host や OS の設定を所有しない。

Cluster 全体の infrastructure として必要な、

* control plane endpoint と infrastructure の関係
* cluster-level infrastructure readiness
* infrastructure-wide configuration

のみを担当する。

Cluster API の InfraCluster contract を自然に実装することを優先し、Tart 独自設定を不必要に増やさない。

---

## TartMachine

Cluster API Machine に対応する InfraMachine Resource。

TartMachine は、

```text
CAPI Machine
      ↕
TartMachine
      ↕
TartHost
```

の binding を表す。

主な責務は、

* Host selection / allocation
* desired Talos image
* Host lifecycle
* bootstrap data の delivery
* ProviderID
* Machine address
* Talos installation readiness
* infrastructure readiness

とする。

Provisioning の細かい step は保持しない。

---

## TartMachineTemplate

MachineDeployment、ControlPlane、ClusterClass などから TartMachine を生成する template。

通常の CAPI template semantics に従う。

---

## TartBootstrapConfig

Talos machine configuration を生成する BootstrapConfig Resource。

Control plane と worker の双方で使用する。

ユーザーが指定する Talos-native configuration と、Cluster API から得られる cluster-specific 情報を組み合わせ、対象 Machine に適用可能な最終 machine configuration を生成する。

---

## TartBootstrapConfigTemplate

MachineDeployment 等から TartBootstrapConfig を生成する template。

---

## TartControlPlane

Control Plane Provider contract を実装する。

以下を所有する。

* control plane Machine の desired replica count
* control plane Machine lifecycle
* control plane health
* Talos etcd bootstrap
* Kubernetes version lifecycle
* control plane rollout
* in-place control plane update
* control plane scale up/down
* quorum を考慮した安全性

CNI や application workload は所有しない。

---

## TartControlPlaneTemplate

ClusterClass 等から利用するための template。

---

# 8. Host discovery と enrollment

ユーザーが OS 起動前に disk UUID、Linux device path、NIC 名などを把握していることを要求してはならない。

Host の初期登録では、可能な限り少ない情報で開始できるようにする。

典型的には、

```text
Host identity
MAC address
必要なら power control configuration
```

程度から開始できることが望ましい。

未構成 Host は network boot 等によって Talos maintenance environment を起動できる。

Talos が起動した後、Tart は Talos API を利用して hardware inventory を取得する。

```text
minimal host registration
        ↓
Talos maintenance boot
        ↓
hardware discovery
        ↓
TartHost.status に inventory を反映
        ↓
Machine configuration を安全に決定可能
```

という流れとする。

これによりユーザーは install 前から `/dev/sda`、disk UUID 等を調査する必要がない。

---

# 9. Initial Provisioning

新しい Machine の基本 lifecycle は次の通りとする。

```text
CAPI Machine 作成
        ↓
TartMachine 作成
        ↓
条件に合う TartHost を claim
        ↓
Host を起動
        ↓
Talos maintenance environment 起動
        ↓
Host identity / hardware を確認
        ↓
Bootstrap Provider が生成した
Talos machine configuration を取得
        ↓
Talos API で configuration 適用
        ↓
Talos が installation
        ↓
disk boot
        ↓
authenticated Talos API 接続
        ↓
Machine の状態確認
        ↓
ProviderID / Address / Conditions 更新
        ↓
InfrastructureReady
```

Tart 自身が block device へ OS image を書き込んではならない。

Tart 自身が partition table を直接編集してはならない。

インストールは Talos installer に委譲する。

---

# 10. Bootstrap Provider

Bootstrap Provider は Talos machine configuration の生成を担当する。

Bootstrap Provider は OS installation の実行自体を担当しない。

生成される configuration は少なくとも、

```text
Cluster identity
Machine role
Cluster endpoint
Talos PKI
Kubernetes PKI / bootstrap information
desired Kubernetes version
user supplied Talos configuration
machine-specific configuration
```

を正しく合成したものでなければならない。

## Talos-native configuration

ユーザーが Talos の設定能力を失わないことを最優先とする。

Tart API が Talos machine configuration 全体を再定義してはならない。

Talos machinery が提供する型や patch/configuration model を可能な限り利用する。

ユーザー設定は「Tart が知っている subset」だけに制限してはならない。

新しい Talos feature が追加された際、それを利用するために Tart の CRD schema を毎回拡張する必要がある設計は避ける。

---

# 11. Control Plane Provider

Control Plane Provider は Talos control plane の lifecycle を完全に管理する。

## 初回 bootstrap

新規 cluster では複数 control plane Machine が Talos configuration を受け取った後、適切な1台に対して一度だけ Talos etcd bootstrap を行う。

bootstrap は idempotent に扱う。

controller 再起動や一時的な API failure により、既に bootstrap 済みの cluster に対して危険な再初期化を行ってはならない。

## HA control plane

1 node、3 node などの構成をサポートする。

control plane endpoint の実装方式を Tart 固有に固定しない。

したがって kube-vip を利用可能でなければならないが、

```text
spec.kubeVIP.enabled
```

のような kube-vip 専用 API は Tart に作らない。

kube-vip は Talos configuration や CAPI の addon/bootstrap mechanism から構成できればよい。

## Scale

scale up / down では etcd membership と quorum safety を考慮する。

特に control plane scale down は単なる Machine deletion として扱わず、Talos/etcd の状態を確認した安全な lifecycle を提供する。

---

# 12. Storage Design

Storage configuration は Talos の disk/volume model を正本とする。

Tart 固有の partition DSL を作成してはならない。

ユーザーは Talos が提供する、

* system volume configuration
* user volume
* raw volume
* disk selector
* encryption
* installer disk selection

などを利用できる。

例えばユーザーが、

```text
NVMe
  OS
  EPHEMERAL
  IMAGECACHE

SSD
  Longhorn

HDD
  TopoLVM

別の HDD
  application local storage
```

のような構成を Talos が許す範囲で作成できなければならない。

---

# 13. Disk selection

Linux の `/dev/sda` や `/dev/nvme0n1` のような不安定な名前を API の基本 identity としない。

Talos の disk inventory と disk selector を利用する。

可能な場合、

```text
serial
WWID
model
transport
size
rotational
bus information
その他 stable attribute
```

によって selection できること。

Tart は hardware discovery の結果をユーザーへ見せ、適切な Talos disk selector を作ることを補助してよい。

ただし selector helper は UX であり、Tart controller の domain model ではない。

---

# 14. Kubernetes Add-ons

Tart は add-on manager にならない。

以下は Tart の専用 API に組み込まない。

* Cilium
* Longhorn
* TopoLVM
* kube-vip
* CoreDNS customization
* metrics-server
* ingress controller
* observability stack

一方、それらを利用するために Talos configuration が必要な場合は、その Talos configuration を TartBootstrapConfig から指定できなければならない。

例えば、

```text
kube-proxy 無効化
system extension
kernel module
mount
RawVolume
UserVolume
kubelet configuration
extra manifest
```

などを Talos-native configuration として利用できるようにする。

Kubernetes Resource の installation は ClusterResourceSet、Addon Provider、GitOps 等の CAPI/Kubernetes 側の一般的な mechanism へ委譲する。

---

# 15. Update Architecture

更新は新 Tart の最重要機能である。

以下の種類を明確に区別する。

```text
Talos OS update
Talos machine configuration update
Kubernetes update
Machine replacement
```

すべてを「Machine rollout」として一括処理してはならない。

---

# 16. CAPI in-place update

Tart は Cluster API の in-place update mechanism を正式な lifecycle API として利用する。

CanUpdateMachine / CanUpdateMachineSet / UpdateMachine 等の CAPI Runtime Extension contract に従う。

更新可能な差分については既存 Machine を更新する。

```text
desired Machine
       ↓
in-place update decision
       ↓
same CAPI Machine
same TartMachine
same TartHost
       ↓
Talos API
       ↓
updated Node
```

Machine replacement を通常更新の仕組みとして使用しない。

---

# 17. Update safety policy

Infrastructure / Bootstrap configuration の field は概念的に、

```text
In-place mutable
Initial-only
Destructive
Identity
```

に分類して扱う。

### In-place mutable

Talos API により安全に既存 Machine へ反映可能なもの。

例:

* Talos version
* Kubernetes lifecycle に関連する desired state
* reboot を伴って適用可能な machine configuration
* reboot 不要の machine configuration

### Initial-only

初回 installation 後に変更できない、または変更すべきでないもの。

例:

* installation target を根本的に変える設定
  -既存データを破壊する disk topology change

### Destructive

データ損失の可能性がある変更。

暗黙に適用してはならない。

### Identity

TartHost binding 等、Machine identity 自体に関係するもの。

通常 update の一部として変更してはならない。

---

# 18. Fail closed

Tart にとって非常に重要な原則として、

> in-place update できない = Machine replacement してよい

とはしない。

CAPI が immutable rollout に fallback し得ることを前提として、Tart が保護すべき Machine では destructive replacement が暗黙に開始されない設計とする。

安全に更新できない変更は、

```text
Blocked
RequiresExplicitReprovision
UnsafeChange
```

等の明確な Condition / error としてユーザーに返す。

特に local persistent storage を持つ Machine について、単なる template 差分から Host の再初期化や disk wipe へ進んではならない。

---

# 19. Talos OS Upgrade

Talos version の変更は原則として in-place operation とする。

```text
desired Talos image
        ↓
current Talos version/image を観測
        ↓
必要なら Talos upgrade API
        ↓
reboot
        ↓
Talos API recovery
        ↓
health check
        ↓
complete
```

Talos 自身の upgrade / rollback 機構を利用する。

Tart 独自の A/B partition、BootTrial、rollback partition manager 等を実装してはならない。

Talos が要求する intermediate version が存在する場合は、安全な upgrade path を選択する。

---

# 20. Machine Configuration Update

Talos machine configuration の変更も、Talos API が既存 Machine に安全に適用可能なら in-place update とする。

変更によって、

```text
immediate apply
service restart
reboot
```

などが必要になる可能性があるが、その判断は可能な限り Talos の semantics に従う。

Tart が Talos configuration field ごとの巨大な独自 compatibility table を持つことは避ける。

一方、disk layout など destructive になり得る configuration は安全側に倒し、自動適用しない。

---

# 21. Kubernetes Upgrade

Kubernetes version は Talos OS version と別の desired state として扱う。

Talos OS upgrade を行っただけで Kubernetes version を暗黙に変更しない。

Control Plane Provider は Kubernetes の cluster-wide lifecycle を所有する。

Cluster API の upgrade sequencing / upgrade plan と Talos の Kubernetes upgrade semantics を整合させる。

重要なのは、

```text
CAPI desired Kubernetes version
```

を正本とし、Talos 側で独立した version drift を発生させないことである。

Talos の Kubernetes upgrade operation が cluster 全体へ影響する点を考慮し、Control Plane Provider と worker Machine lifecycle が矛盾しないように orchestration すること。

Control plane と worker の desired state が最終的に同一の CAPI 宣言状態へ収束することを保証する。

CAPI が提供する upgrade plan / lifecycle extension を必要に応じて利用し、Kubernetes version skew policy に従う。

---

# 22. Rolling Update

複数 Machine が存在し capacity に余裕がある場合、更新は可能な限り availability を維持する。

例えば3 node cluster では、

```text
Node A update
  ↓ healthy
Node B update
  ↓ healthy
Node C update
```

のように進める。

同時に停止可能な Machine 数は CAPI の rollout policy と整合させる。

Tart 独自の `maxUnavailable` 類似機構を重複して作成しない。

---

# 23. Single Node Cluster

1 node cluster を正式にサポートする。

1 node cluster では更新時の downtime を許容する。

ただし、

```text
Machine delete
Host clean
Machine recreate
```

による update を行ってはならない。

同一 Machine / TartMachine / TartHost / disk を維持したまま、

```text
drain where possible
→ Talos update
→ reboot
→ recovery
```

を実行する。

availability より data preservation を優先する。

---

# 24. Local Persistent Data

Tart の設計では、Machine 上に重要な local state が存在する可能性を常に考慮する。

具体例:

```text
Longhorn replica
TopoLVM LV
local PersistentVolume
application data
container image cache
その他 node-local state
```

通常の upgrade ではこれらを保持する。

Tart は「Kubernetes workload があるから delete しても scheduler が何とかする」と仮定してはならない。

---

# 25. Machine Deletion

Update と Delete は明確に別 semantics とする。

ユーザーが明示的に Machine deletion / scale down を行った場合、CAPI Machine 自体が削除されることは許容する。

しかし TartMachine deletion が即座に disk wipe を意味してはならない。

デフォルトでは physical Host のデータを保持する安全な挙動を選択する。

概念として、

```text
Machine deleted
      ↓
Host unbound
      ↓
existing data retained
      ↓
Host requires explicit reuse/reprovision decision
```

とする。

Host を別 Machine に再利用するための destructive cleaning は明示的な操作として扱う。

---

# 26. Host Allocation

TartMachine は特定 TartHost を直接指定する方法と、条件から適切な Host を割り当てる方法の双方を提供できること。

Host selection では少なくとも、

* architecture
* labels
* hardware capability
* availability

等を考慮可能とする。

ただし scheduler を独自に大規模実装することを目的にしない。

単純で deterministic な allocation を優先する。

同一 Host が複数 Machine に同時 claim されてはならない。

---

# 27. Power Management

Power management は capability として扱う。

対象 Host に応じて、

```text
Wake-on-LAN
BMC / Redfish
VM API
manual / unmanaged
```

等を将来的に利用できる。

Infrastructure lifecycle が特定の power backend を前提としないこと。

Power control が存在しない Host でも、ユーザーが手動で起動できるなら provisioning を継続可能な設計が望ましい。

---

# 28. Boot

Tart は Host を Talos maintenance environment へ起動できる仕組みを持つ。

ただし network boot implementation の具体方式を architecture として固定しない。

重要なのは、

```text
Host identity
        ↓
適切な Talos boot asset
        ↓
Talos maintenance environment
```

を安全に提供できることである。

bootstrap secret や cluster credential を firmware boot protocol に直接露出させない。

初期 boot asset は可能な限り secret-free とする。

---

# 29. Talos Image

OS artifact に独自 Tart format を導入しない。

Talos が扱う installer image / boot asset / Image Factory 等の概念を利用する。

desired state では再現可能な image identity を扱えること。

可変 tag のみを永続的 identity として信頼する設計を避け、可能な範囲で immutable な artifact identity を使用する。

architecture や platform 固有の Talos image が必要な場合にも同じ lifecycle model を維持する。

---

# 30. Security Model

## Initial trust

普通の PC には TPM attestation や BMC identity が存在しない場合がある。

したがって初回 provisioning では「完全な hardware identity を暗号学的に証明できる」と仮定してはならない。

初期 provisioning network は trusted infrastructure として明確に security boundary に含める。

Host identification には利用可能な範囲で、

```text
MAC
system UUID
hardware inventory
その他 stable identity
```

を使用する。

---

## Maintenance mode

未構成 Talos maintenance API は bootstrap のためだけに利用する。

その trust model と制約を明示的に理解した上で使用し、初期 provisioning 後は authenticated Talos API に移行する。

---

## Secrets

以下を CR Status、Event、通常 log、error message に含めない。

* Talos machine secrets
* Kubernetes PKI private key
* Talos client key
* Bootstrap Data
* kubeconfig
* token
* credential
* BMC password
* private signing material

Secret は Kubernetes Secret 等、秘密情報を保持するための mechanism に格納する。

---

## Least privilege

Infrastructure Provider、Bootstrap Provider、Control Plane Provider が必要以上の Kubernetes permission を持たないようにする。

特に network boot を提供するコンポーネントと cluster administrative credential を扱うコンポーネントが同一 trust boundary である必要はない。

具体的な process 分割は実装者が判断する。

---

# 31. Status と Conditions

巨大な `status.phase` enum を lifecycle の中心にしない。

CAPI v1beta2 contract と Kubernetes API convention に沿った Conditions を中心とする。

Resource ごとに、

```text
Ready
Available
Claimed
InfrastructureReady
BootstrapReady
TalosReachable
Provisioned
UpToDate
Updating
Healthy
```

等の意味のある observed state を必要に応じて表現する。

Condition は「controller 内部の何番目の step にいるか」ではなく、「外部から観測可能な能力・状態」を表す。

`observedGeneration` を適切に扱う。

---

# 32. Error Handling

期待される環境エラーは panic や永久 failure にしない。

例:

```text
Host の電源が入らない
Talos API がまだ起動していない
DHCP address がまだ判明しない
Host inventory がまだ取得できない
Node reboot 中
Kubernetes API が一時的に unavailable
```

これらは reconcile 可能な transient state として扱う。

一方、

```text
destructive configuration change
unsupported update path
identity mismatch
invalid storage selector
unsafe control plane operation
```

などは明確な Condition として停止させる。

Controller 再起動後に同じ処理を再実行しても安全であること。

---

# 33. Idempotency

すべての external operation は reconcile loop から安全に再試行できる設計にする。

例えば controller が、

```text
Talos upgrade request
```

を送信した直後にクラッシュしても、

再起動後に observed Talos version を確認することで、

```text
未開始
実行中
完了済み
失敗
```

を判断できなければならない。

「API call を送った事実」を process memory や Operation Resource の step number だけで記録する設計を避ける。

---

# 34. Source of Truth

各情報について正本を明確にする。

```text
Cluster topology
    → Cluster API

Host inventory / assignment
    → TartHost

Machine desired infrastructure
    → TartMachine

Talos desired configuration
    → TartBootstrapConfig / CAPI desired state

Talos actual configuration
    → Talos API

Talos actual version
    → Talos API

Kubernetes desired version
    → Cluster API

Kubernetes actual state
    → workload Kubernetes API / Talos API

disk actual state
    → Talos API
```

同じ状態を複数 Resource に独立した正本として重複保存しない。

Status に保存する場合は cache / observation として扱う。

---

# 35. Provider Boundaries

## Infrastructure Provider

Infrastructure Provider が所有するもの:

```text
Host
Host allocation
power
boot
Talos installation delivery
Machine infrastructure identity
ProviderID
addresses
Talos OS lifecycle の infrastructure 側
```

## Bootstrap Provider

Bootstrap Provider が所有するもの:

```text
Talos machine configuration generation
cluster secrets
node role configuration
user Talos configuration の合成
bootstrap data Secret
```

## Control Plane Provider

Control Plane Provider が所有するもの:

```text
control plane replica
etcd lifecycle
initial bootstrap
control plane health
Kubernetes lifecycle
control plane upgrade sequencing
control plane scaling safety
```

責務をまたぐ共通ライブラリを利用してよいが、Provider contract 上の ownership は明確に保つ。

---

# 36. CAPI Contract

実装時点の最新 Cluster API v1beta2 Provider contract に従う。

過去 Tart API や旧 v1beta1 implementation との互換性を維持する必要はない。

古い API を温存するために設計を複雑化してはならない。

必要なら既存 CRD、controller、internal package、test を削除し、新 architecture に適したものへ置き換える。

Infrastructure、Bootstrap、Control Plane の各 Provider contract を正式に実装し、CAPI core controller が期待する ownership、references、Conditions、readiness semantics、deletion semantics に従う。

---

# 37. ClusterClass

ClusterClass / managed topology から自然に利用できることを設計要件とする。

Tart 固有の installation path を利用しないと cluster を作成できないような設計にはしない。

Cluster topology から、

```text
TartCluster
TartControlPlane
TartMachineTemplate
TartBootstrapConfigTemplate
MachineDeployment
```

等を通常の CAPI Resource として構成できること。

CAPI の chained upgrade / upgrade plan と統合可能な設計とする。

---

# 38. Observability

ログは Kubernetes reconciliation の原因と結果が追跡できる structured log とする。

秘密情報を含めない。

Kubernetes Event はユーザーが行動を取る必要がある重要な lifecycle event に限定する。

再試行のたびに同じ Event を大量発行しない。

Metrics は少なくとも、

```text
reconcile
Talos API
Host availability
provisioning
upgrade
failure
```

等の運用状態を観測可能にすることが望ましい。

具体的な metric schema は実装時に決定する。

---

# 39. 将来拡張

将来以下を追加可能であること。

### Proxmox VM

Host backend として VM create/delete/power/boot を追加できること。

ただし物理 Host と VM の差を無理に隠す巨大な共通 abstraction は作らない。

### ARM / Raspberry Pi

architecture/platform に応じた Talos boot asset を選択可能にする。

Machine lifecycle 自体は x86 bare metal と同じモデルを利用する。

### Secure Boot / TPM

将来的に Secure Boot、TPM、attestation 等を追加できる。

ただしそれらを必須条件にしない。

---

# 40. 明示的に禁止する設計

新実装では、明確な必要性が証明されない限り以下を導入しない。

```text
独自 Provisioning Agent
独自 Node Lifecycle Agent
TartHostOperation
Workflow engine
Command → Event framework
Event sourcing
Provisioning Plan CRD
独自 OS image format
独自 partition DSL
独自 A/B updater
独自 disk writer
独自 rollback manager
Cilium 専用 API
Longhorn 専用 API
TopoLVM 専用 API
kube-vip 専用 API
巨大な Host phase state machine
process memory を正本とする operation tracking
```

抽象化は将来的に必要そうだから作るのではなく、現在存在する複数の具体的ユースケースから必要性が確認できた場合のみ追加する。

---

# 41. 実装判断の原則

この文書は実装のディレクトリ構成、package 構成、具体的な PXE server、DHCP implementation、HTTP framework、内部 interface の数等を指定しない。

それらは実装者がコード量、testability、security boundary、既存 ecosystem を考慮して決定する。

ただし、どの実装方法を選択しても以下の原則を崩してはならない。

1. Talos の機能を不要に再実装しない。
2. CAPI の標準 lifecycle を不要に再実装しない。
3. Machine update で local persistent data を暗黙に破壊しない。
4. Controller を再起動しても安全に reconcile を継続できる。
5. Talos configuration の柔軟性を Tart API が制限しない。
6. Add-on 固有の知識を Infrastructure Provider に持ち込まない。
7. 危険な操作では availability より data safety を優先する。
8. 暗黙の Machine replacement より明示的な failure を選ぶ。
9. Resource Status を workflow program counter として使用しない。
10. 既存コードとの互換性を理由に新 architecture を妥協しない。

---

# 42. 完成条件

新 Tart の最初の実用可能な architecture は、少なくとも以下を E2E で成立させること。

### Fresh machine

まっさらな Host を登録し、Cluster API Resource を作成するだけで Talos が install され Kubernetes cluster が Ready になる。

### Single node

1 control-plane Machine の cluster を作成できる。

Talos OS と Kubernetes を同じ Machine のまま更新できる。

再起動中は downtime が発生してよい。

既存 disk data は保持される。

### HA control plane

複数 control-plane Machine の cluster を作成できる。

安全に scale up/down できる。

1台ずつ Talos update できる。

### Worker

MachineDeployment から worker を作成できる。

既存 Machine を削除せず in-place update できる。

### Storage

複数 disk を持つ Host について、Talos-native disk selector と volume configuration により異なる用途へ disk/partition を割り当てられる。

### Hardware discovery

ユーザーが事前に disk UUID や Linux device path を知らなくても、maintenance Talos から得た inventory を利用して storage configuration を作成できる。

### Add-ons

Tart に Cilium/Longhorn/TopoLVM 固有 API を追加することなく、それらに必要な Talos configuration と Kubernetes manifests を利用できる。

### Recovery

controller-manager を provisioning / reboot / upgrade の任意のタイミングで停止・再起動しても、Resource を手動修復せず reconciliation を継続できる。

### Safety

通常の Talos/Kubernetes update が CAPI Machine replacement、Host cleaning、disk wipe を引き起こさない。

安全に実行できない変更については破壊的 fallback を行わず、ユーザーへ明確に blocked state を報告する。

---

# 43. 最終的な設計思想

Tart の中心思想は次の一文で表せる。

> Tart は、永続的な Host 上で動く Talos Linux Machine を Cluster API に統合し、Host allocation・boot・Talos configuration delivery・control plane lifecycle・安全な in-place update を提供する Provider である。Talos Linux が既に提供する OS、storage、upgrade、rollback、node management の仕組みを再実装しない。

さらに、Machine lifecycle では次の原則を最優先する。

> Desired state を変更しただけで、ユーザーの知らないところで永続データを持つ Machine が破棄されてはならない。

通常の変更は可能な限り同じ Machine 上で収束させる。

in-place で安全に収束できない変更は明示的に停止する。

破壊的な reprovisioning は、通常 update の fallback ではなく、ユーザーが意図して開始する別の lifecycle operation として扱う。

この原則を Infrastructure Provider、Bootstrap Provider、Control Plane Provider のすべてで一貫して維持すること。
