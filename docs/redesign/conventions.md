# 記述規約と用語集

## 1. この文書の目的

この文書は、`docs/redesign`以下の文章を人間とAI Agentが同じ意味で解釈するための共通規約である。ほかの文書で用語の定義を省略している場合は、この文書の定義を適用する。

実装者は、ここで定義されていない抽象語を新しく使う前に、次のいずれかを行わなければならない。

1. この文書へ定義を追加する。
2. 使用箇所で入力、処理、出力、失敗条件を定義する。
3. 未決定事項として、決定するタスクと判定基準を記載する。

## 2. 要求レベル

文書中の要求レベルは次の意味で使用する。

| 表現 | 意味 |
|---|---|
| 必須 | 受け入れ条件を満たすために実装しなければならない。例外を認める場合はADRが必要 |
| 禁止 | 実装してはならない。例外を認める場合はADRが必要 |
| 推奨 | 原則として採用する。採用しない場合はタスクまたはPRに理由を記録 |
| 任意 | 実装の有無が受け入れ条件へ影響しない |
| 未決定 | 現時点では実装方針を選ばない。決定タスクと判定基準の記載が必須 |
| 対象外 | 現在の計画では実装しない |

「適切に」「安全に」「十分に」「必要に応じて」「可能であれば」だけで要求を記載してはならない。これらを使う場合は、直後に判定できる条件を列挙する。

## 3. 文書の優先順位

同じ事項について記述が矛盾する場合は、次の順で優先する。

1. `Accepted`のADR
2. [達成すべき状態](target-state.md)
3. [アーキテクチャ](architecture.md)
4. [全体の実装計画](implementation-plan.md)
5. 個別タスク文書
6. `Proposed`のADR

矛盾を発見した実装者は、上位文書に黙って合わせて実装してはならない。矛盾する文書を同じ変更で修正する。`Proposed`のADRは実装を拘束せず、記載された検証が完了するまで選択肢として扱う。

## 4. システムとリソース

| 用語 | 定義 |
|---|---|
| 管理クラスタ | CAPI、Provider、`TartHost`等のCRを実行・保存するKubernetesクラスタ |
| ワークロードクラスタ | 本Providerが物理ホスト上へ構築するKubernetesクラスタ |
| CAPI rollout owner | Machineの作成、削除、更新順を決めるcontroller。control planeではKubeadmControlPlane、workerではMachineDeployment |
| Infrastructure Provider | このリポジトリで実装するProvider。物理ホスト割当、電源・boot操作、OS配置、InfraMachine状態を担当 |
| Bootstrap Provider | Machineをクラスタへ参加させるBootstrap Dataを生成するProvider。kubeadmではCABPK |
| Control Plane Provider | control planeのversion、replica、更新順を管理するProvider。kubeadmではKCP |
| `TartHost` | 1台の物理ホストを表す長寿命CR。Machineを削除しても自動削除しない |
| `TartMachine` | 1つのCAPI Machineに対応するInfraMachine CR。CAPI Machineと同時に作成・削除する |
| `TartHostOperation` | 1回のProvision、Update、Rollback、Cleanを表すCR。長時間処理の再開位置を保存 |
| controller | 管理クラスタ上のcontroller-manager process。Reconcileと組み込みnetwork serverを実行 |
| Provisioning Agent | 一時OS上で動作し、diskの検出、partition作成、image書き込み、検証を行う実行ファイル |
| Node Lifecycle Service | インストール済みOS上でone-shot起動し、`kubeadm upgrade`等のdistribution固有処理を行う実行ファイル。任意command実行APIは持たない |

## 5. 境界を表す用語

| 用語 | 定義 |
|---|---|
| Port | application層が外部副作用を呼び出すためのGo interface |
| Adapter | Portを満たす具体実装。例: WoL adapter、Redfish adapter、Kubernetes adapter |
| Driver | 物理機器の操作を実装するAdapterの総称。Power、BootOverride、VirtualMediaの各Portを必要な分だけ実装 |
| Capability | Driverが実行できる操作を表す列挙値。例: `PowerOn`、`PowerOff`、`ObservePowerState`、`SetNextBoot`、`VirtualMedia` |
| Platform Profile | architecture、firmware、boot transport、partition role、bootloader、Agent artifactの組を識別するversion付き設定 |
| Distribution Lifecycle Driver | kubeadmまたはk3sの更新前検査、snapshot、適用、検証を実行するPort/Adapter |
| Boot Transport | Provisioning Agentを起動する経路。`IPXE`、`RedfishPXE`、`RedfishHTTPBoot`、`RedfishVirtualMedia`、`RaspberryPiEEPROM`のいずれか |

「plugin」は外部processで動くgRPC実装にだけ使用する。controllerに組み込むGo実装は「Adapter」または「Driver」と呼ぶ。

## 6. Diskと永続データ

| 用語 | 定義 |
|---|---|
| Boot | bootloader、kernel、initramfs/UKI、slot選択情報を保存する論理領域 |
| OS-A / OS-B | root filesystem imageを保存する2つの論理領域 |
| Active Slot | 現在起動しているOS-AまたはOS-B |
| Inactive Slot | Active Slotではない方のOS領域。更新時の書き込み先 |
| Verity-A / Verity-B | 対応するOS slotのdm-verity hash treeを保存する論理領域 |
| State | node identityとcluster参加状態を保存するread-write領域 |
| Data | container runtime、kubelet volume、利用者が指定したPV path等の大容量read-write領域 |
| Disk Role | Boot、OS-A/B、Verity-A/B、State、Dataという論理的な用途 |
| Physical Layout | Disk RoleをGPT partitionへ割り当てたPlatform Profile固有の定義 |
| `stateSchema` | State内のdirectory、file、formatの互換version。単調増加する正の整数 |

Stateには、少なくともmachine-id、node credential、`/etc/kubernetes`または`/etc/rancher/k3s`、Bootstrap適用済みmarkerを保存する。Dataには、少なくともcontainer runtime data、kubelet data、etcd data、明示されたstorage pathを保存する。最終的なpath一覧はOSとdistributionの組ごとにPlatform Profileで固定する。

## 7. Artifactと認証データ

| 用語 | 定義 |
|---|---|
| OS Artifact | OS filesystem image、verity metadata、manifestを含むdigest固定OCI Artifact |
| Agent Artifact | Provisioning Agentを含むkernel/initramfsまたはbootable ISO |
| Artifact Manifest | OS、architecture、distribution、size、digest、stateSchema、generation、verity root hashを記録する署名対象データ |
| Bootstrap Data | Bootstrap Providerが生成し、Secretの`value`へ保存するbyte列 |
| Bootstrap Bundle | Bootstrap Data、`format`、payload digest、Machine UID、Operation IDを1つにまとめた配信単位 |
| Initial Credential | Provisioning Agentが最初のsessionを開始するためのcredential |
| Session Token | Host UIDとOperation UIDへ結び付けた256 bit以上の乱数。保存時はSHA-256 hashだけを保持 |
| Plan | 1つのOperationでAgentまたはNode Lifecycle Serviceが実行する命令を列挙した署名対象データ |
| Plan Digest | Planをcanonical serializationしたbyte列のSHA-256 digest |
| Artifact Generation | 古い署名済みArtifactへの巻き戻しを防ぐための単調増加整数 |

「digest」は明記がない限りSHA-256を意味し、`sha256:<64桁の小文字16進数>`形式で表す。可変tagだけのOCI参照は禁止する。

## 8. Operationと状態

| 用語 | 定義 |
|---|---|
| Operation ID | UUIDとして生成し、1つの`TartHostOperation`の生存期間中は変更しない識別子 |
| Phase | Operation全体の現在位置を表す列挙値 |
| Step | Phase内で1回だけ実行する副作用。完了したStep名をStatusへ保存 |
| Retry | 同じOperation IDと同じ入力でStepを再実行すること |
| Requeue | controller-runtimeが同じKubernetes objectを再度Reconcileすること |
| Commit | 新slotを今後の既定boot先として確定し、Operationを`Succeeded`にする処理 |
| Rollback | State schemaを変更していない更新で、既定boot先を旧slotへ戻す処理 |
| Recovery | Snapshot復元またはoperator操作が必要な状態。自動Rollbackと区別する |
| Health Gate | Commit前に全て満たす必要がある検査項目の集合 |

冪等とは、同じOperation ID、Plan Digest、Stepを複数回受け取っても、外部状態が1回成功した場合と同じ状態へ収束することを意味する。「APIを2回呼ばない」という意味ではない。

## 9. 更新クラス

| 更新クラス | 定義 | 自動Rollback |
|---|---|---|
| `OSOnly` | Kubernetes version、Bootstrap Data、stateSchemaを変更せず、OS Artifactだけを変更 | 必須 |
| `KubernetesBinary` | version skew範囲内でKubernetes binaryと設定を変更し、不可逆なState migrationを行わない | 検証で許可された組合せだけ可能 |
| `StateMigration` | kubeadm、etcd、k3s等がState/Dataのformatを変更 | 禁止。Snapshotを使うRecoveryへ遷移 |

更新クラスはPlan作成時にcontrollerが決定し、Agentが推測してはならない。

## 10. 完了と失敗

| 表現 | 判定条件 |
|---|---|
| Agent書き込み完了 | imageとverityを書き込み、read-back digestがManifestと一致 |
| OS boot完了 | 指定slotとArtifact Generationで起動し、State/Dataの必須mountが成功 |
| Node health完了 | 対象Nodeの`Ready=True`、providerID一致、期待するKubernetes version一致 |
| Provisioning完了 | OS boot完了、Bootstrap成功marker、Node health完了の全てが成立 |
| OS-only更新完了 | OS boot完了、Node health完了、slot Commitが成立 |
| Operation失敗 | 再試行可能回数またはdeadlineを超過し、`Failed`または`RecoveryRequired`へ遷移 |

「成功」「Ready」「完了」は、対象となる上記の判定条件を特定して使用する。

## 11. 時間、回数、上限値

次の値はAPIまたはcontroller設定へ明示し、コードへ散在させない。

| 項目 | 初期値 | 規則 |
|---|---|---|
| Session Token TTL | 10分 | 発行時刻から計測。延長せず、再発行時は別token |
| Session認証失敗上限 | 5回 | Host UIDとOperation UIDの組ごと。超過後はtokenを失効 |
| 外部APIの1回のtimeout | 30秒 | Redfish等で別値が必要な場合はDriver設定に明記 |
| 一時エラーの再試行 | 最大3回 | 初期待機1秒、倍率2、jitter有り。Operation全体の再試行とは別 |
| boot trial回数 | 3回 | 3回ともHealth Gate前に失敗したら旧slotへ戻す |
| Operation deadline | Planの必須フィールド | 未設定のPlanを拒否。Operation種別ごとの値はTask 01で決定 |

値を変更する場合は、対応するテストと運用文書を同じ変更へ含める。

## 12. 未決定事項の書き方

未決定事項は次の形式で記載する。

```text
未決定:
- 選択肢: A / B
- 決定タスク: Task NN
- 判定基準: 測定値または合否条件
- 決定まで禁止する実装: ...
```

「今後検討する」「将来対応する」だけを記載してはならない。
