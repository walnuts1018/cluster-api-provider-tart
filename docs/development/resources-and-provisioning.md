# リソースと Provisioning の流れ

この文書は、Tart の実装を読むときに、Kubernetes Resource と対象 Host 側の処理がどのように結び付くかを
説明する。パッケージの責務は [アーキテクチャ](architecture.md) を参照する。

## Resource の役割

| Resource | 所有する情報 | 主な実装箇所 |
|---|---|---|
| `TartHost` | 物理 Host の識別子、boot・power driver、割当、観測状態 | `api/v1beta1/tarthost_types.go`、`domain/provisioning` |
| `TartMachine` | CAPI Machine に対応する希望状態と Host 参照 | `api/v1beta1/tartmachine_types.go`、`infrastructure/k8s_controller` |
| `TartHostOperation` | Provisioning、更新、cleaning の入力、進捗、再開位置 | `api/v1beta1/tarthostoperation_types.go`、`domain/provisioning/workflow` |

`TartHost` は物理資産の inventory であり、Machine を削除しても自動で消えない。`TartMachine` は CAPI の
Infrastructure Machine として Machine ごとの希望状態を表す。副作用を伴う処理の詳細は `TartHostOperation`
だけに保存し、Host と Machine には参照と要約状態だけを保持する。

## Host と Operation の状態

`TartHost.status.phase` は Host の利用状態を示す。主な安定状態は `Available`、`Provisioned`、`Retained`、
`Detached` であり、処理中には `Reserved`、`Provisioning`、`Updating`、`Cleaning` になる。自動処理を
継続できない場合は `RecoveryRequired` または `Error` を使用する。

`TartHostOperation.status.phase` は再開可能な副作用の位置を示す。通常の流れは次のとおりである。

```text
Pending
  -> PreparingBoot
  -> WaitingForAgent
  -> Writing
  -> Verifying
  -> BootTrial
  -> AwaitingHealth
  -> Succeeded
```

更新で Node Lifecycle Service が必要な場合は `AwaitingHealth` と `BootTrial` の間に
`DistributionUpdating` が入る。失敗は `Failed`、手動復旧が必要な場合は `RecoveryRequired`、
旧 slot へ戻す処理は `RollingBack` として記録する。これらの Phase、完了済み Step、Agent report の
sequence は `TartHostOperation.status` に保存されるため、controller の再起動後も同じ Operation を再開できる。

## 初期 Provisioning のデータフロー

```text
TartMachine Reconcile
  -> Host の割当と TartHostOperation の作成
  -> Power / Boot Driver で対象 Host を起動
  -> ProxyDHCP と TFTP が iPXE を配信
  -> HTTPS が Provisioning Agent を起動する情報を配信
  -> Agent が登録し、署名済み Plan を取得
  -> Agent が disk 書込みと検証を実行し、progress を報告
  -> boot report と Kubernetes の health を確認
  -> Operation と Host の Status を更新
```

controller-manager に組み込まれた ProxyDHCP、TFTP、HTTPS server は boot 経路を提供する。
Provisioning Agent は対象 Host の一時環境で実行され、disk layout、payload 書込み、read-back 検証、
boot trial を担当する。Agent が Kubernetes API を直接操作することはない。

Agent API は `infrastructure/http_server/agentapi` にあり、登録、Plan 取得、progress、boot report、
Bootstrap Data、Node Lifecycle Plan の endpoint を扱う。入力 DTO は `dto/agent`、署名済み Plan の
検証と状態遷移は Domain Workflow と Kubernetes repository を経由して行う。

## Artifact、disk、更新

OS Artifact は OCI Artifact として扱い、可変 tag ではなく digest 固定参照を使用する。Provisioning Agent は
Platform Profile と Plan に従い、disk を論理的に Boot、OS-A、Verity-A、OS-B、Verity-B、State、Data の
役割へ構成する。更新では inactive slot へ書き込み、boot trial と health 確認を通過してから新しい slot を
確定する。State と Data は OS slot と分離し、Node identity、Kubernetes state、永続データを OS 更新から守る。

実機用 Provisioning Agent Artifact の initramfs は、Ubuntu/Debian installer が初期導入で扱う範囲を基準に、
一般的な PCI・USB・仮想 NIC、SATA/NVMe/SCSI/RAID/USB/仮想 storage、device mapper、software RAID、
主要 filesystem の kernel module と依存 firmware を含める。`/init` はその module をロードしてから DHCP を
実行する。これにより firmware が iPXE で利用できても Linux kernel 側が NIC や storage controller を認識しない
差異を避ける。特定機種用の out-of-tree driver や無線 LAN の認証は Artifact の責務に含めない。

`UpdateClass` は `OSOnly`、`KubernetesBinary`、`StateMigration` を区別する。Plan を作成する controller が
更新種別と対象 slot を決め、Agent が推測してはならない。

## セキュリティ境界

- Bootstrap Data は Bootstrap Provider が作成する。Tart は配送に必要な format と digest を扱うが、内容を
  Domain の設定として解釈しない。
- Agent の Session Token は operation と Host に結び付ける。Kubernetes には平文ではなく hash と期限だけを
  保存する。
- Bootstrap Data は single-shot 配信であり、受領済みの operation へ再配信しない。
- Agent の progress と boot report は operation と Plan digest に照合する。Plan にない Step や古い sequence の
  report は状態遷移に使用しない。
- Secret、Session Token、Bootstrap Data、署名鍵、payload は CR Status、log、テスト artifact に出力しない。

認証・配信を変更する場合は、`domain/agentdelivery`、`infrastructure/http_server/agentapi`、
`infrastructure/repository/k8s` の実装と Contract Test を同時に確認する。
