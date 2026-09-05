# Talos連携

TartはTalos Linux専用であり、Talosが提供するOS、storage、configuration、upgrade、rollback、etcd bootstrap、Kubernetes runtimeを再実装しない。Tartの責務はCAPIのdesired stateとTalos APIを安全に接続することである。

---

## Machine Configuration の合成パイプライン

Bootstrap Providerは、以下の順序で決定論的にmachine configurationを合成する。

```text
cluster secret bundleとCAPI/Tartコンテキストからbase configurationを生成
        ↓
user-owned Talos patchを適用（configSecretRef経由）
        ↓
Provider-owned invariantを適用（上書き不可）
        ↓
effective configurationをシリアライズし、SHA-256 digestを計算
```

### Provider-Owned Invariants（保護対象）
以下の項目はProviderが整合性を所有するため、user patchによって変更することはできない。user patchがこれらのfieldと競合した場合は上書きせず、`Ready=False`、`Reason=ConfigurationConflict` として安全停止する。
- `TartCluster.spec.id`、Talos PKI、machine token
- cluster endpoint
- CAPI Machineから導出されたmachine role（controlplane / worker）
- CAPI Machineから導出されたKubernetes component version
- Tartが生成したProviderID（`tart://host/<ID>`）
- `TartMachine.spec.talosImage` のinstaller image identity

`patches` keyのraw patchはBootstrap Providerがactive bundleとCAPI Machine contextから生成したbaseへ適用する。Provider-owned invariantとの競合は`Ready=False`、`Reason=ConfigurationConflict`として安全停止する。

---

## Talos Image と System Extension

- **正本の管理**: desired imageの正本は `TartMachine.spec.talosImage` の `{version, schematicID}` である。
- **System Extension**: 必要なsystem extension（各種ドライバ、ツール等）はImage Factoryのschematicへ組み込み、BootstrapConfigのpatchと二重に所有しない。
- **boot assetとinstaller**: PXE/ISOなどのboot assetとTalos installer imageには同じversionとschematicを使用する。

---

## ストレージとボリュームの扱い

- **Talos-native セマンティクス**: Tart独自のpartition DSLやdisk writerを作らず、Talosのsystem volume、user volume、raw volume、disk selector、encryption、installer diskの仕組みをそのまま利用する。
- **安定したディスク識別子**: Linuxの `/dev/sda` や `/dev/nvme0n1` などのデバイス名は起動順序で変わり得る一時的な観測値とし、serial、WWID、model、transport、bus情報などのstable attributeを識別・セレクターの基盤とする。
- **初回install target**: `TartBootstrapConfig`はclaimed `TartHost.status.inventory`から書き込み可能なdiskを一意に選択し、Talos v1.14の`UnattendedInstallConfig`へstable CEL selectorを追加する。候補を一意に選べない場合はconfigurationを生成せず停止する。
- **永続データの分離**: `EPHEMERAL` パーティションはOSアップグレードや再インストールで揮発し得るため永続データの保持先として扱わない。Longhorn、TopoLVM、アプリケーション永続データはUser VolumeまたはRaw Volumeへ明示的に分離する。

---

## ハードウェア探索 (Hardware Discovery)

- ユーザーにinstall前の `/dev/sda`、`/dev/nvme0n1`、NIC名、disk UUID等の事前調査を要求しない。
- Bootstrap SecretなしのDiscovery bootにより、maintenance Talos APIからCPUアーキテクチャ、system UUID、NIC、アドレス、disk詳細を取得し、`TartHost.status.inventory` へ観測として保存する。
- 現在の組み込みcontrollerはDHCP、TFTP、PXEのendpoint discoveryを持たないため、maintenance APIのendpointは`TartHost.spec.talosAPIAddress`または外部boot連携が設定した`status.addresses`から供給する。
- disk identityの重複を観測した場合は、関係するHostをallocationとconfiguration applyから除外する。

---

## Maintenance API の Trust Model

未構成（未インストール）のTalosが提供するmaintenance APIは、TLSで暗号化されているものの自己署名証明書であり、クライアント認証のない状態である。
誤ったHostへのconfiguration適用や誤接続を防ぐため、configurationをapplyする前に以下の情報を単一のboot attemptへ結びつけて検証する。

```text
expected TartHost
        ↔ boot attempt
        ↔ MAC / DHCP lease / network endpoint
        ↔ 観測されたsystem UUIDおよびhardware inventory
```

- claimed Hostの `spec.macAddress` とmaintenance APIから観測された物理MACが一致する場合のみconfigurationを適用する。
- 曖昧さや不一致がある場合は適用を停止し、`Ready=False`、`Reason=IdentityConflict` を設定する。
- configuration適用・再起動後は、相互TLSで保護された認証済みTalos APIへ移行する。

---

## Installation と Upgrade の委譲

- **OSインストール**: Tart自身はブロックデバイスへのimage書き込みやパーティション分割を行わず、Talos installerへconfigurationを渡して委譲する。
- **In-place Upgrade**: Talos v1.13以降のLifecycle APIへdesired installer imageを渡してOSアップデートを行う。controllerは認証済みAPIで観測したversionとschematic identityを`TartMachine.status`へ保存し、Talosが古いversionまたはsystem extension setへロールバックした場合はdesired Specを追従させず、`UpdateMachine`を`Failure`、`Reason=RolledBack`として安全停止する。Lifecycle APIを利用できない古いversionや完了statusを取得できない応答は更新を開始しない。
- **Kubernetes Upgrade**: Talosのcluster-wide `upgrade-k8s` を利用する経路は、cluster-wide orchestrationと完了観測を実装するまでRuntime Extensionで安全停止する。古いconfigurationの再applyやMachine単位のOS upgradeでKubernetesコンポーネントを変更しない。

---

## Kubernetes Add-on との境界

- Cilium、Longhorn、TopoLVM、kube-vip、CoreDNS customization、metrics-server、ingress、observability stackなどをTart専用のAPIとしてリポジトリ内に組み込まない。
- 必要なTalos設定はBootstrapConfigのraw patchまたはimage schematicで指定し、Kubernetesリソースの配布はClusterResourceSet、Addon Provider、GitOpsなどへ委譲する。

---

## API アダプター境界 (`talos` パッケージ)

[`talos/`](../../talos) パッケージは、Talos gRPCクライアント、認証情報、maintenance/authenticatedモードの切り替えをカプセル化する。
コントローラーはTalosの内部gRPC型に直接依存せず、アダプターが提供する小さなドメイン観測型（Reachable, Version, Healthy, Disks等）を通じて状態を判断する。
安全な接続判定ロジックのタスクは[実装タスク一覧 (タスク4)](tasks.md)を参照。
