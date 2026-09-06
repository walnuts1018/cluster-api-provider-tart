# Machine lifecycle

この文書は、Tartにおけるリソースのライフサイクル、状態遷移、および安全規則の正本（SSOT）である。

Tartのlifecycleは、Kubernetes resourceのdesired stateと、Host、Talos、workload clusterのobserved stateの比較から毎回再計算する。長時間処理のstep、Operation CRD、メモリ内の途中状態を正本にしない。

---

## Host Allocationと状態分類

Hostのallocation eligibilityは、以下の4つの観測状態で分類される（実装は[`host/eligibility.go`](../../host/eligibility.go)を参照）。

```text
Available
  │ spec.consumerRef と spec.retainedFrom がなく、自動allocation可能
  ▼
Claimed
  │ spec.consumerRef があり、特定のTartMachineに排他的に割り当て済み
  ▼ (Machine削除後)
Retained
  │ spec.retainedFrom が残り、ディスクやTalos設定が残存。自動allocation不可
  ▼ (明示的な再利用承認)
Reusable
  │ spec.reusePolicy: Reusable、一致する spec.reuseApproval.retainedFromUID、
  │ および spec.reuseMode (Adopt または Reprovision) が揃った状態
```

### Claimと排他制御

- `TartHost.spec.consumerRef` をcontroller管理のbindingとする。
- claimはSSAではなく、resourceVersion付きUpdateまたはJSON Patchの `test` によるatomic CASで確立する（[`host/claim.go`](../../host/claim.go)）。
- 競合が発生した場合は上書きせず、別Hostの選択または再試行を行う。

### Recovery IdentityとHost上のinstallationの寿命

Machineの寿命と、Host上のTalos installationの寿命は一致しない。Retained Hostを安全に扱うためのrecovery identityは、TartMachineやTartBootstrapConfigのownership lifecycleから独立した寿命を持つ。

- **Recovery Secret**: provider管理namespace上のimmutable Secret（`tart-talos-recovery-<cluster-id>-<ca-fingerprint>`）であり、Talos API（machine/OS）CAのcertificateとprivate key、およびcluster IDだけを保持する。Kubernetes PKI、service account key、bootstrap token、Bootstrap Data全体は複製しない。実装は[`recovery/secret.go`](../../recovery/secret.go)を参照する。
- **共有単位**: 「Talos cluster identity → recovery Secret → それに属する複数のTartHost」という構造であり、同じ旧clusterに属するHostごとにCA private keyを複製しない。CA rotationで有効なCAが変わった場合だけ別のSecretになる。
- **確立のタイミング**: Machine削除の瞬間にSecretを退避するのではなく、Talos configurationをHostへapplyする前に確立する。`TartMachineReconciler`はcluster secret bundleのactive generationからrecovery Secretを作成し、`TartHost.status.currentTalosIdentityRef`へbindingを書く（[`controller/reprovision.go`](../../controller/reprovision.go)の`ensureTalosIdentityBinding`）。
- **binding更新の規則**: 既存bindingが別clusterを指す場合は決して上書きしない。それはそのHostが保持する旧installationをresetできる唯一の根拠である。同一cluster内でCA rotationが完了した場合だけ、現在のactive identityへ更新する。
- **GC**: `TartCluster`やMachineのOwnerReferenceでGCしない。[`controller/talosrecovery_controller.go`](../../controller/talosrecovery_controller.go)が定期的に現在のTartHost参照を観測し、そのrecovery Secretを参照する`status.currentTalosIdentityRef`が1つも存在しない場合だけ削除する。参照countは保持せず、毎回のreconcileでTartHost集合から再計算する。

### Reusableの2つの動作

- **`Adopt`**: 既存のTalosインストール、同一Cluster ID、同一secret generation、同一Host identity、同一ProviderID、整合するrole/versionが一致する場合のみ、データを保持したままclaimする。control-planeのAdoptではetcd membershipとNode identityも検証する。
- **`Reprovision`**: ユーザーが明示的にデータ破棄を承認した別ライフサイクルであり、recovery identityで旧Talos APIへ認証してからTalosのreset/installer機構へ委譲し、maintenance mode復帰を確認した上で新しいMachineへclaimする。
- 通常のallocationやupdate、deleteのフォールバックとして暗黙に実行してはならない。
- **自動Reprovisionの前提要件**: installed OSが存在する状態からremoteにmaintenance environmentへ戻せるboot strategyをHostが持つ必要がある。Fresh machineのnetwork boot capabilityだけでは自動Reprovisionを許可しない。
- Reusable Hostを再利用できるのは`TartMachine.spec.hostRef`による明示的な指定経路だけである。自動選択（[`host.SelectFreshForFailureDomain`](../../host/selection.go)）はAvailable Hostしか選ばない。

### Reprovision Flow

```text
TartHost = Retained（spec.previousConsumerRef が残る）
        ↓ ユーザーが spec.reusePolicy: AllowReuse、spec.reuseMode: Reprovision、
        ↓ 現在の previousConsumerRef.uid に一致する spec.reuseApproval を設定
        ↓ TartMachine.spec.hostRef で明示的に指定し、consumerRef をatomic CASでclaim
        ↓ status.currentTalosIdentityRef が指す recovery Secret を解決
        ↓ recovery CAから短命な os:admin client certificate を発行（既定10分）
        ↓ 旧Talos APIへ相互TLSで接続（server certificateがrecovery CAに属することの検証を含む）
        ↓ active machine configuration から cluster ID を観測して照合
        ↓ inventory から MAC address と system UUID を観測して照合、endpointの一致も確認
        ↓ すべて一致した場合のみ Talos Reset（WipeMode=ALL、user disk は対象外）
        ↓ 旧identityで認証できなくなり、maintenance APIで期待したHost identityを確認
        ↓ status.currentTalosIdentityRef を解除（bindingの解放）
        ↓ 通常のfresh provisioning（reconcileMaintenanceTalos）
        ↓ 新しいcluster identityへ status.currentTalosIdentityRef を再確立
```

- 実装は[`controller/reprovision.go`](../../controller/reprovision.go)の`reconcileReprovision`であり、[`controller/tartmachine_controller.go`](../../controller/tartmachine_controller.go)の`reconcileTalos`から`reconcileAuthenticatedTalos`より前に分岐する。
- Statusをworkflowのstep番号として使わず、毎回「recovery identity bindingの有無」と「旧Talos API／maintenance APIの到達性」を再観測して継続位置を決める。controller再起動後も同じ観測から安全に再開できる。
- identityが1つでも一致しない場合はResetを実行せず、`Ready=False`、`Reason=IdentityConflict`でrequeueせずに安全停止する。MAC addressだけ、IP addressだけを根拠にResetしてはならない。
- recovery Secretを解決できない場合は`Reason=RecoveryIdentityUnavailable`で停止する。

### Reset Scope

Talos Resetがwipeするのは、Talos自身のsystem installation（system disk上のSTATE/EPHEMERALなどのsystem partition）である。[`talos.Client.Reset`](../../talos/client.go)は`WipeMode=ALL`を明示し、`UserDisksToWipe`を指定しない。

したがってLonghorn、TopoLVM、raw volumeなど別diskまたはuser diskとして構成されたデータはこの操作の対象外であり、**Reprovision後に全データが消えたと仮定してはならない**。それらのデータの扱いはユーザーまたはstorage add-onの責務である。

---

## Fresh Machine Provisioning

```text
CAPI Machine / TartMachine作成
        ↓
Hostの選択とTartHost.spec.consumerRefのatomic CASによるclaim
        ↓
TartHost.spec.id由来のProviderID（tart://host/<ID>）をTartMachineへ設定
        ↓
Discovery boot: secret-freeなmaintenance Talos APIへ接続し、ハードウェアインベントリを取得
        ↓
Bootstrap Secret（完全なTalos machine configuration）の到着を待機
        ↓
Talos maintenance APIへconfigurationをapplyし、OS installを実行
        ↓
Host再起動後、認証済み相互TLS Talos APIへ接続
        ↓
Talos version、健全性、ProviderIDを観測し、TartMachineのInfrastructureReadyを反映
```

- **DiscoveryとProvisioningの分離**: Discovery bootはBootstrap Secretを待たずに実行可能だが、Talosへのconfiguration applyやOS install、provisioning用電源操作はBootstrap Secretが存在するまで開始しない。これによりハードウェア探索と構成適用の循環依存を防ぐ。
- 詳細な未実装タスクは[実装タスク一覧 (タスク4, 6)](tasks.md)を参照。

---

## In-Place Update と Rollout

Tartでは、同一のMachine、TartMachine、TartHost、diskを維持したin-place updateを基本とする。更新は以下の4種類に分類される。

| 更新種別 | 更新方法 | 安全性の判定 |
| --- | --- | --- |
| **Talos OS version/image** | Talos upgrade APIを呼出 | desired image、再起動後の接続性、健全性で完了判定 |
| **Talos machine configuration** | `TartBootstrapConfig.spec.updatePolicy.configuration`に従いTalos APIでapply（必要ならcontrolled reboot） | effective configurationのdiffを「data、identityを破壊するか」で分類し、破壊しない差分のみ適用 |
| **Kubernetes version** | 未実装のため安全停止 | cluster-wide orchestrationと完了観測を実装するまでRuntime Extensionがpatchなしで拒否 |
| **Host identity / 破壊的disk変更 / InitialOnly configuration** | 自動更新不可 | `ReprovisionRequired`として安全停止（Machine replacementへfallbackしない） |

### in-place updateとreboot-free updateの区別

in-place updateとreboot-free updateは別概念である。rebootが必要であっても、同一CAPI Machine、同一TartMachine、同一TartHost、同一local storageのまま「configuration apply → controlled reboot → health recovery」で完結するなら、それは完全なin-place updateである。Machine replacementへは決してfallbackしない。

### Configuration Update Policy

`TartBootstrapConfig.spec.updatePolicy.configuration`（および`TartBootstrapConfigTemplate`の同名field）は、raw patchの差し替えによって生じるeffective machine configuration差分の適用方針を表す。

| Policy | 動作 |
| --- | --- |
| **Auto**（既定） | Talos 1.14はreboot要否を信頼できる形で判定できないため、`Auto = Reboot`として扱う。「安全かもしれないからreboot-freeを試す」楽観的動作は行わない。将来Talosが信頼できる判定APIを提供した場合に備え、この判定は[`update/policy.go`](../../update/policy.go)の`autoResolvesToReboot`へ分離してある。 |
| **Live** | ユーザーがrunning systemへのlive applyを明示的に宣言したadvanced option。Talosのreboot-free apply（`ApplyConfigurationRequest_NO_REBOOT`）を使い、失敗してもRebootへ自動fallbackせず明示的な`Failure`で停止する。 |
| **Reboot** | configuration applyの後にTartがrebootをorchestrateする。複数node clusterでは既存のcordon/drainと`TartCluster.spec.updatePolicy.disruptionPolicy`（`allowDowntime`）に従って一度に安全な台数だけ更新し、control planeはetcd quorum判定と組み合わせる。single-node clusterではdowntimeを許容する。 |
| **InitialOnly** | 初回provisioning後に変更してはいけないconfigurationを表す。差分を検出した場合は`ReprovisionRequired`として安全停止し、Bootstrap Secretも作り直さない。 |

### Destructive configurationの扱い

判定基準は「rebootが必要か」ではなく「data、identityを破壊するか」である。disk layout、installation target、既存volumeのwipe/recreate、Talos cluster identity/PKIの置換、machine roleの根本的変更は通常のupdateから除外し、`ReprovisionRequired`として安全停止する。判定は[`update/configuration.go`](../../update/configuration.go)がTalos machineryのtyped configurationとdocument kindから行い、未知のconfiguration document kindはdestructive側（安全側）へ倒す。

### Update Extensionによる保護

- 初回provisioning後のmutableなTalos OS/config変更を実行できるのはUpdate Extension（Runtime Extension）のみとする。
- `CanUpdateMachineSet` / `CanUpdateMachine` は完全なdiffを評価し、Talos imageとBootstrapConfigのraw patch参照（およびupdate policy）の変更だけを `Success` + 完全なpatchで許可する。危険・未知・部分的な差分は patchなしの `Failure` で確実にvetoする。InitialOnly policy下でのraw patch変更もここでvetoする。
- Secretの内容に依存するdestructive判定は、Secretを観測できる `UpdateMachine` で行い、`ReprovisionRequired`として停止する。
- `UpdateMachine`は「RPCが成功した」ことを完了条件にしない。Talos APIの到達性、desired configurationの反映、（rebootを伴う場合は）boot時刻の変化、Talos serviceのhealth、workload cluster上のNode Readyまで観測してから完了とする。Statusにはprogram counterやstep番号を保存せず、controller再起動後も外部観測から再計算する。

### Workerの標準Rollout Profile

- Workerの対応するCAPI `RollingUpdate` 設定では、`maxSurge: 0`、`maxUnavailable: 1` を推奨する。
- `maxSurge: 0` により追加Hostを必要とせず、既存Machineを1台ずつin-place updateできる。`maxUnavailable: 0` ではCAPIがバッファのためsurge Machineを作成してしまい追加Hostが必要になるため、ローカルHostを保護する既定値としない。
- `OnDelete` strategyは自動worker in-place update lifecycleとしてサポートしない。
- Control PlaneはMachineDeploymentへ委譲せず、常に1台ずつ更新してetcd、API、Node healthを確認する。

### Kubernetes Upgradeの収束規則（実装前の契約）

Talosの`upgrade-k8s`はcluster-wide operationであり、現在のProviderはMachine単位のRuntime Extensionから実行しない。以下はcluster-wide orchestrationを実装する際の契約であり、現状のKubernetes version差分はpatchなしで安全停止する。

```text
Topology managed cluster:
  CAPI upgrade planが目標version Xのcontrol-plane/worker stepと整合する状態を開始
  ↓
  TartControlPlaneがstepを確認し、talos upgrade-k8s Xをcluster単位で一度だけ要求
  ↓
  Kubernetes API、全Nodeのkubelet、control plane actual versionがXになることを観測
  ↓
  TartControlPlane status.versionsをXへ更新
  ↓
  CAPIがworkerのMachine.spec.versionをXへ伝播
  ↓
  worker UpdateMachineがactual version Xを観測し、重複upgradeなしで完了
```

- **Topology managed**: worker `Machine.spec.version` が旧versionであっても、CAPI upgrade planとskewが整合していれば `upgrade-k8s` を開始できる。MachineDeploymentの `maxUnavailable` でupgrade availabilityを制御しない。
- **directly managed**: `TartControlPlane.spec.version` の変更がトリガーとなる。worker desired versionが目標versionと矛盾する場合は開始前に `Ready=False`、`Reason=VersionSkew` で安全停止する。
- **component imageの保護**: full configurationの再apply時に、current CAPI `Machine.spec.version` を必ずversion-managed fieldへ反映し、古いconfigurationでKubernetes componentがダウングレードされるのを防ぐ。

### Downtime許容ポリシー (`allowDowntime`)

- node-disruptiveな更新では、まずTalosの安全なdrainまたはcordon/drainを試みる。
- drain失敗がavailability、PDB、capacityだけの理由であり、かつ `TartCluster.spec.updatePolicy.allowDowntime: true` が明示されている場合のみ、graceful shutdown/rebootを許容する。未指定または `false` の場合は安全停止する。
- 破壊的disk変更、identity不一致、etcd membership違反、quorum違反は `allowDowntime` でも緩和しない。

---

## Deletion と Retention

Machine削除時は、物理Hostの暗黙的なデータ破棄や即時解放を行わない。

```text
CAPI Machine controllerがdrainおよびvolume detachを完了
        ↓
scale-down時: Control Plane Providerがpre-terminate delete hookでetcd member removalを完了
        ↓
TartMachine finalizerがTalos APIへ安全なshutdown/quiesceを要求
        ↓
Hostが停止したことを観測して確認
        ↓
TartHost.spec.retainedFromへ直前のconsumer UIDとCluster IDを記録
        ↓
TartHost.spec.consumerRefを解除し、HostをRetainedとして保持
```

### 停止確認の責務

- **BMC / VM backend**: out-of-bandで電源状態（Redfishでは`PowerState=Off`）を確認する。
- **WoL-only / manual backend**: authenticated Talos `Shutdown` RPCの受理後に、Talos API endpointが一定時間消失したことを観測する（物理電源OFFの証明ではなく、旧clusterへ接続可能なTalosが動作し続けていないことの確認）。
- 停止が確認できない場合はclaimとfinalizerを保持し、`Ready=False`、`Reason=ShutdownUnconfirmed` を設定する。
- **Cluster全体の削除**: 個別のscale-downではquorum維持とmember removalが必須だが、Cluster全体の削除では削除不能を避けるためmember removal完了を必須とせず、hookを安全に完了させる。

---

## MachineHealthCheck の保護

Tartはローカルボリュームやadd-onの状態を安全に判別できないため、すべてのTart-managed MachineでMHCのdelete-and-recreate remediationを既定で抑止する。

- MachineSetまたはControl PlaneのMachine templateのmetadataへ、生成前から `cluster.x-k8s.io/skip-remediation: "true"` を設定する。
- Tart v1alpha1では自動replacementを提供しない。利用者が手動でMachineを削除した場合は、別のAvailable Hostがclaimされる可能性がある。

---

## Recovery と Transient Error

- power off、DHCP address待ち、maintenance API待ち、Talos reboot、Kubernetes APIの一時的unavailableはreconcile可能なtransient errorとして扱い、backoff付きでrequeueする。
- identity mismatch、invalid selector、destructive change、quorum violation、rollback、停止未確認のdeletionはbounded retryを続けず、外部副作用を止めて `Ready=False` と具体的なreasonへ反映する。
- 外部API call直後にコントローラーが再起動しても、次回reconcileでversion、reachability、health、configuration digest、ProviderID、etcd membership、Secret、Node状態を再観測して安全に継続できるようにする。
