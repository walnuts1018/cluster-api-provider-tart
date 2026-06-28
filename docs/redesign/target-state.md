# 達成すべき状態

## 1. プロダクトとしての完了状態

利用者が物理ホストを`TartHost`として登録し、CAPIのCluster、MachineDeployment、KubeadmControlPlane等を操作することで、次のライフサイクルを宣言的に管理できる。

1. 利用可能な物理ホストを競合なく予約する。
2. ホストの能力に応じてWoL、Redfish等で電源と次回ブート方法を制御する。
3. iPXEまたはRedfish Virtual Mediaから一時プロビジョニング環境を起動する。
4. 検証済みOSスロットイメージを対象ディスクへ配置する。
5. CAPI Bootstrap Providerが生成したデータを一度だけ取得し、永続State領域へ安全に配置する。
6. 同じ物理ホスト、ノードID、証明書、etcd、コンテナランタイム、PVデータを維持したまま、OSスロットとKubernetes配布物を更新する。
7. OS-only等のrollback可能classは旧スロットへ自動ロールバックし、State migration後の失敗はsnapshot/recoveryへ遷移する。いずれも失敗理由をCondition、Event、メトリクス、トレースで確認できる。
8. Machine削除時はポリシーに従ってデータ消去または保持を行い、ホストを再利用可能に戻す。

## 2. 対応範囲

| 項目 | 最終目標 | 初期の実用リリース |
|---|---|---|
| Kubernetes | kubeadm、k3s | kubeadm |
| OS | Ubuntu 24.04 LTS、Ubuntu 26.04 LTS、Debian 13 | Ubuntu 24.04 LTS |
| CPU | amd64、arm64 | amd64 |
| ファームウェア | UEFI、Legacy BIOS、Raspberry Pi固有ブート | UEFI |
| 電源・ブート | WoL+iPXE、Redfish PXE、Redfish Virtual Media | WoL+iPXE |
| 更新 | A/B更新、Kubernetes lifecycle、ヘルス確認、ロールバック | workerのOS-only A/B更新 |
| 外部拡張 | versioned gRPC driver | Go組み込みdriver |

Ubuntu 26.04 LTSは2026年4月23日にリリース済みだが、古いamd64機で使う成果物はCPU命令セットを明示してビルド・試験する。クラウド向けイメージの既定値を、そのまま第2世代Intel Core相当の対応根拠にしてはならない。

## 3. 永続性の契約

### OSスロット

- `OS-A`と`OS-B`は対応するVerity metadataからdm-verity deviceとして検証し、read-onlyでマウントする。
- 稼働スロットは書き換えない。
- `apt upgrade`、`do-release-upgrade`、稼働ルート上でのKubernetesパッケージ更新はサポートしない。
- 新しいOS・Kubernetes実行物は新しいスロットイメージとして供給する。
- 受理済みartifact generationを管理側とStateの両方へ保持し、古い正規署名imageへの意図しないrollbackを拒否する。

### State

ノードのアイデンティティとクラスタ参加状態を保存する。少なくとも次をOSスロット外へ分離する。

- kubeadm: `/etc/kubernetes`、kubeletのPKI・設定、コントロールプレーンではetcdデータ
- k3s: `/etc/rancher/k3s`、`/var/lib/rancher/k3s`内のノード・クラスタ状態
- machine-id、ホスト固有ネットワーク設定、初回Bootstrapの完了記録

State全体をread-onlyにはしない。証明書ローテーション等で継続的に書き込むパスと、一度配置後に保護できるBootstrap原本を分離する。

### Data

再生成可能だが容量の大きいランタイムデータ、および利用者が明示した永続データを保存する。

- コンテナランタイムのデータ
- kubeletのPod・volume状態
- local-path-provisioner、Longhorn等の指定ディレクトリ

StateとDataの互換性は成果物マニフェストの`stateSchema`で管理する。互換範囲外のイメージへの更新は開始前に拒否する。

### 削除・再利用

- `WipeAll`: State/Data/slot metadataを消去し、hostを`Available`へ戻す。
- `RetainData`: node identityを含むStateを消去する。Dataは隔離状態で残し、明示的なadoptionまたはwipeなしに新Machineへmountしない。
- `RetainState`: 同じMachineの障害復旧専用とし、hostを`Detached`相当へ置く。新Machineへ割り当てず、自動的に`Available`へ戻さない。

delete-first host reuseで新しいMachineを割り当てる場合は、少なくとも旧State、providerID、node credential、Bootstrap完了markerを初期化する。State保持と「同じNode identityのインプレース更新」を混同しない。

## 4. 信頼性と安全性の完了条件

- 同一ホストを2つの`TartMachine`へ割り当てない。予約はKubernetes APIのresourceVersionを用いて競合を検出する。
- Reconcileを任意の段階で再実行しても、ディスク書き込み、電源操作、トークン消費が重複して破壊的結果を生まない。
- ディスク対象は`/dev/disk/by-id`等の安定識別子と期待するserial/sizeの組で確認し、一致しない場合は書き込まない。
- OS成果物、プロビジョニング環境、計画書はdigestで固定し、署名検証ポリシーを通過したものだけを使用する。
- Bootstrap bundleはTLS上で短命・推測困難・単回利用のトークンにより1回のレスポンスとして取得する。ログ、Status、EventにSecretまたは生トークンを残さない。
- 更新は「inactive slotへの書き込み」「検証」「次回のみ起動」「ノード健全性確認」「確定」の順で行う。
- 起動試行回数を超えた場合は旧スロットへ戻る。単にプロセスが起動しただけでは更新成功としない。
- OS-only互換更新、state migrationを伴わないKubernetes binary更新、control-plane/etcd migrationを分類し、最後の分類はsnapshotと明示的recovery planなしに自動rollback可能と宣言しない。
- 電源状態を取得できないWoL driverは`Unknown`を正しい結果として返し、pingを電源状態の真実として扱わない。

## 5. CAPIとの完了条件

- v1beta2 InfraMachine contractの必須フィールドと初期化完了条件を満たす。
- CAPIから渡されるBootstrap Secretを参照し、独自のkubeadm/k3sクラスタ初期化仕様をInfraMachineへ持ち込まない。
- Bootstrap bundleはSecretの`value`と`format`を保持し、初期MVPではstandard CABPK `cloud-config`だけを対応済みとする。
- 通常の置換更新を壊さず、明示的に選択された更新だけをRuntime SDK In-Place Update Hooksで処理する。
- 現在使用するCAPI v1.13.1ではRuntimeSDK/InPlaceUpdatesがAlphaかつ既定無効であるため、delete-firstで同じhostを再利用する安定経路を残す。
- `CanUpdateMachine`は実際に安全に処理できる差分だけを覆うpatchを返し、`UpdateMachine`は永続化済みの現在状態から冪等に進行する。
- Runtime Extensionが停止しても既存クラスタの通常Reconcileを妨げず、更新要求はタイムアウトと観測可能な失敗へ収束する。
- Kubernetes version更新はCAPI rollout owner（control planeはKCP、workerはMachineDeployment）が決める順序に従い、Distribution Lifecycle adapterが既存node上のpreflight、snapshot、`kubeadm upgrade`相当、health確認を実行する。

## 6. 運用上の完了条件

- 対応マトリクスごとに、初期導入、再起動、失敗復旧、A/B更新、ロールバック、削除・再利用を実機または同等の仮想ベアメタル環境で検証する。
- 破壊操作には対象host、disk ID、image digest、operation IDを含む監査Eventを残す。
- 各長時間処理に期限、指数バックオフ、上限回数、キャンセルを持たせる。
- リーダーでないcontrollerがDHCP/TFTP/HTTPの応答主体にならない構成、または複数replicaでも一貫する応答設計を文書化する。
- バックアップ対象はKubernetesリソースだけではない。State/Dataのバックアップ・復元手順と、単一ノード更新前の運用ゲートを提供する。
- 単一ノードの同一Nodeインプレース更新は、Runtime SDKの成熟度と電源断を含む独自E2Eが十分になるまでexperimentalと表示する。

## 7. 非目標

- Ubuntu/Debian上での任意の`apt`操作を許す汎用イミュータブルOS
- overlayfsで書き込み可能なrootを見せる方式
- 初回リリースでの任意WASM/gRPCプラグイン実行
- pingやARPだけによる信頼できる電源状態判定
- 異なるCPUアーキテクチャ間での同一OSイメージ共有
- Infrastructure Providerだけで任意Bootstrap ProviderのKubernetes更新手順を推測・実行すること

## 8. 参照資料

- [Cluster API InfraMachine contract](https://main.cluster-api.sigs.k8s.io/developer/providers/contracts/infra-machine)
- [Cluster API In-Place Update Hooks](https://main.cluster-api.sigs.k8s.io/tasks/experimental-features/runtime-sdk/implement-in-place-update-hooks)
- [Metal3 BareMetalHost state machine](https://book.metal3.io/bmo/state_machine)
- [Ubuntu 26.04 LTS release notes](https://documentation.ubuntu.com/release-notes/26.04/)
