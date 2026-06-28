# 達成すべき状態

## 1. この文書の完了条件

この文書は、全タスク完了時に外部から観測できる状態を定義する。用語と要求レベルは[記述規約と用語集](conventions.md)に従う。

実装詳細は含めない。ただし、「動作する」「安全である」のように合否を判定できない表現は使用せず、Kubernetes Resource、disk、Node、Artifactから確認できる条件を記載する。

## 2. 利用者が実行する操作

利用者が初期Provisioningを開始するために必要な操作は次の3つとする。

1. 物理ホストごとに`TartHost`を作成する。
2. CAPI Cluster、Control Plane、MachineDeployment、対応するBootstrapConfigを作成する。
3. `TartMachineTemplate`でHost selector、Platform Profile、OS Artifact、削除Policyを指定する。

利用者は、対象ホストへのSSH接続、installerの対話操作、disk device名の推測、`kubeadm init/join`の手動実行を行わない。

## 3. 初期Provisioningの完了状態

1つの`TartMachine`について、次の全条件が成立した時だけProvisioning完了とする。

| 対象 | 必須状態 |
|---|---|
| CAPI Machine | `spec.providerID`が設定済み |
| TartMachine | `status.initialization.provisioned=true` |
| TartMachine | `Ready=True`、`reason=Provisioned` |
| TartHost | `spec.consumerRef.uid`がTartMachine UIDと一致 |
| TartHost | `status.phase=Provisioned` |
| TartHostOperation | `status.phase=Succeeded` |
| Disk | OS-AまたはOS-Bの一方がActive Slot |
| Disk | Active Slotのread-back digestとArtifact Manifestが一致 |
| OS | dm-verityを経由してroot filesystemをread-only mount |
| OS | State/Dataの必須mountがcontainer runtimeとkubeletより先に成功 |
| Bootstrap | State内の成功markerにBootstrap payload digestを保存 |
| Node | `Ready=True` |
| Node | `spec.providerID`がCAPI MachineのproviderIDと一致 |

Provisioning Agentがdisk書き込みを報告しただけではProvisioning完了にしない。Bootstrap取得だけ、OS起動だけ、kubelet process起動だけでも完了にしない。

## 4. 更新の完了状態

### 4.1 OSOnly更新

次の全条件が成立した時だけOSOnly更新完了とする。

1. Inactive Slotへ新しいOS ArtifactとVerity metadataを書き込んでいる。
2. 旧Active Slot、State、Dataへ書き込んでいない。
3. 新slotのArtifact Generationが更新前より大きい。
4. 新slotでOS boot完了とNode health完了が成立している。
5. boot trial情報を消去し、新slotを既定boot先へCommitしている。
6. `TartMachine.status.activeSlot`と`installedImageDigest`が実diskと一致している。
7. `TartHostOperation.status.phase=Succeeded`である。

OS boot完了またはNode health完了がPlanのdeadlineまでに成立しない場合は、最大3回のboot trial後に旧slotへRollbackする。Rollback後はOperationを`Failed`にし、失敗したArtifact Generationを自動再試行しない。

### 4.2 KubernetesBinary更新

OSOnly更新の条件に加え、次を必須とする。

1. CAPI rollout ownerが指定したnode順を変更していない。
2. Target Kubernetes versionがversion skew policy内である。
3. Node Lifecycle Serviceの`Preflight`、`Apply`、`Verify` Stepが各1回成功している。
4. workerではcontrol planeがTarget versionを受理した後に更新している。
5. control planeではSnapshot取得後にtarget versionの`kubeadm upgrade apply`を実行している。
6. Node、control plane component、etcd quorumのHealth Gateが成立している。

### 4.3 StateMigration更新

StateMigration更新では自動Rollbackを成功条件に含めない。失敗した場合は次の状態へ遷移する。

- `TartHostOperation.status.phase=RecoveryRequired`
- `Ready=False`
- Conditionの`reason`は失敗Stepを識別できる固定列挙値
- `status.snapshotRef`に復元対象を記録

Snapshotが作成されていないStateMigration Planは開始しない。

## 5. 対応範囲

状態は次の3種類で表示する。

| 状態 | 意味 |
|---|---|
| Supported | 対応するE2Eを継続実行し、リリース時の退行をblockする |
| Experimental | feature gateが必要。失敗時の互換性を保証しない |
| Planned | タスクは定義済みだが、利用者向け機能として公開しない |

### 5.1 初期実用リリース

| 項目 | 値 | 状態 |
|---|---|---|
| OS | Ubuntu 24.04 LTS | Supported |
| Kubernetes distribution | kubeadm | Supported |
| CPU architecture | amd64、x86-64-v1 | Supported |
| Firmware | UEFI | Supported |
| Boot Transport | iPXE | Supported |
| Power Driver | WoL | Supported |
| 更新 | workerのOSOnly A/B更新 | Experimental |
| 外部Driver | なし。組み込みGo Adapterだけ | Supported |

### 5.2 最終目標

| 項目 | 追加対象 |
|---|---|
| OS | Ubuntu 26.04 LTS、Debian 13 |
| Kubernetes distribution | k3s |
| CPU architecture | arm64 |
| Firmware | Legacy BIOS、Raspberry Pi 4/5 EEPROM boot |
| Boot Transport | RedfishPXE、RedfishHTTPBoot、RedfishVirtualMedia |
| Power Driver | Redfish |
| 更新 | control plane、単一ノード、KubernetesBinary、StateMigration Recovery |
| 外部Driver | versioned gRPC plugin |

Ubuntu 26.04成果物はx86-64-v1でビルドし、Intel Sandy Bridge実機またはQEMUの`-cpu SandyBridge`でboot testを行う。Ubuntu cloud imageの既定CPU levelを本Providerの対応根拠として使用しない。

## 6. Diskと永続性

### 6.1 OS Slot

- OS-AとOS-Bはdm-verity device経由でread-only mountする。
- Active Slotへの書き込みを禁止する。
- 稼働OS上の`apt upgrade`と`do-release-upgrade`を禁止する。
- OS/Kubernetes binaryの変更は新しいOS Artifactとして配布する。
- Artifact Generationは単調増加させる。同じgenerationでdigestが異なるArtifactを拒否する。

### 6.2 State

最低限、次をStateへ置く。

| 種別 | Pathまたはデータ |
|---|---|
| Host identity | `/etc/machine-id` |
| kubeadm | `/etc/kubernetes` |
| kubelet identity | kubelet client certificate、kubeconfig、設定 |
| k3s | `/etc/rancher/k3s` |
| Bootstrap | payload digest、成功marker |
| 更新 | 受理済みArtifact Generation、Lifecycle Step marker |

State全体をread-onlyにしてはならない。証明書rotationやkubelet設定更新に必要なpathはread-writeとする。

### 6.3 Data

最低限、次をDataへ置く。

| 種別 | Path |
|---|---|
| containerd | `/var/lib/containerd` |
| kubelet | `/var/lib/kubelet` |
| etcd | `/var/lib/etcd` |
| k3s | `/var/lib/rancher/k3s`のうち大容量可変データ |
| Storage | 利用者がPlatform Profileで列挙したlocal-path/Longhorn path |

最終path一覧はOSとdistributionの組ごとにPlatform Profileへ保存し、`stateSchema`でversion管理する。Manifestの互換範囲に現在の`stateSchema`が含まれない場合は、disk書き込み前にPlanを拒否する。

## 7. Machine削除とHost再利用

削除Policyは次の3値だけを使用する。

| Policy | State | Data | 削除後Host phase | 新Machineへの自動割当 |
|---|---|---|---|---|
| `WipeAll` | 全logical blockをzero overwriteまたはdevice sanitize | 同左 | `Available` | 許可 |
| `RetainData` | 消去 | 保持・隔離 | `Retained` | 禁止 |
| `RetainState` | 保持 | 保持 | `Detached` | 禁止 |

`Retained`/`Detached` Hostは`WipeAll`を実行するまで`Available`へ戻さない。初期リリースでは保持Data/Stateを新MachineへadoptするAPIを提供しない。

delete-first host reuseで新しいMachineを割り当てる場合は`WipeAll`完了を必須とする。旧providerID、Node credential、Bootstrap成功markerを新Machineへ引き継がない。

## 8. 排他性と再実行

- 1つの`TartHost`が参照できるactive `TartMachine`は最大1つ。
- 1つの`TartHost`でterminal Phase (`Succeeded`、`Failed`、`RecoveryRequired`) 以外のOperationは最大1つ。
- Host予約は`metadata.resourceVersion`を含む更新で行い、Conflictを成功として扱わない。
- 各副作用StepはOperation IDとPlan Digestをidempotency keyとして使用する。
- controller再起動後は`TartHostOperation.status.phase`と`completedSteps`から再開し、process memoryだけを判断根拠にしない。

## 9. Security完了条件

- Session Tokenは256 bit以上の暗号学的乱数とし、管理クラスタにはSHA-256 hashだけを保存する。
- Session Token TTLは10分、認証失敗上限は5回とする。
- TokenをURL query、Kubernetes Event、Status、log、trace attributeへ記録しない。
- Bootstrap BundleはTLSで配信し、Host UID、Operation UID、Machine UIDへ結び付ける。
- OS/Agent Artifactはdigest固定OCI参照とし、署名検証後に使用する。
- Secure Boot Profileではverity root hashを署名済みUKIまたは署名対象boot metadataへ含める。
- Secure Bootを持たないProfileはdm-verityを偶発破損検知として表示し、悪意あるdisk改変への耐性を表示しない。
- hardware identityを持たないnetwork-boot-only Hostでは、隔離Provisioning L2を必須要件として利用者文書へ表示する。

## 10. CAPI契約

- InfraMachine v1beta2 contractの必須fieldを実装する。
- `status.initialization.provisioned`は初回Provisioning完了後に`false`へ戻さない。
- 更新中は`Ready=False`と`reason=Updating`で表現する。
- RuntimeSDK/InPlaceUpdatesが無効な場合は通常のMachine置換を妨げない。
- `CanUpdateMachine`と`CanUpdateMachineSet`は、処理できない差分をpatchで覆わない。
- CAPI v1.13.1ではRuntimeSDK/InPlaceUpdatesをAlpha、既定無効として表示する。

## 11. 観測可能性

各Operationについて次を確認できるようにする。

| 出力 | 必須情報 |
|---|---|
| Condition | type、status、reason、英語message、observedGeneration |
| Event | Operation ID、Host名、Operation type、失敗Step。Token/Secretは禁止 |
| Metric | Operation type、Driver名、Phase、result、duration。Host名はlabelにしない |
| Trace | Operation IDでcontroller、Driver、Agent reportを関連付ける |

## 12. リリース判定

Supportedへ変更する組合せごとに、次のE2Eを全て成功させる。

1. 初期Provisioning
2. controller再起動からの再開
3. Agent通信切断からの再開
4. disk書き込み中の電源断
5. 電源断を注入しないOS再起動
6. OSOnly更新
7. 新slot boot失敗からのRollback
8. Machine削除とPolicyどおりのHost状態

KubernetesBinaryまたはStateMigrationをSupportedにする場合は、さらにsnapshot、apply中断、RecoveryのE2Eを追加する。

## 13. 対象外

- 稼働rootへpackage managerで更新を重ねる方式
- OverlayFSでrootをread-writeに見せる方式
- ping/ARPだけからPowerStateを`On`または`Off`と断定する方式
- Infrastructure ProviderがBootstrap Dataを独自生成する方式
- Infrastructure ProviderがCAPI rollout ownerを迂回して複数nodeを同時更新する方式
- 初期リリースでのWASM plugin

## 14. 参照資料

- [記述規約と用語集](conventions.md)
- [Cluster API InfraMachine contract](https://main.cluster-api.sigs.k8s.io/developer/providers/contracts/infra-machine)
- [Cluster API In-Place Update Hooks](https://main.cluster-api.sigs.k8s.io/tasks/experimental-features/runtime-sdk/implement-in-place-update-hooks)
- [Metal3 BareMetalHost state machine](https://book.metal3.io/bmo/state_machine)
- [Ubuntu 26.04 LTS release notes](https://documentation.ubuntu.com/release-notes/26.04/)
