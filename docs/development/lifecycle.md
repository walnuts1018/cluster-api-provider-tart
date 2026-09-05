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

### Reusableの2つの動作

- **`Adopt`**: 既存のTalosインストール、同一Cluster ID、同一secret generation、同一Host identity、同一ProviderID、整合するrole/versionが一致する場合のみ、データを保持したままclaimする。control-planeのAdoptではetcd membershipとNode identityも検証する。
- **`Reprovision`**: ユーザーが明示的にデータ破棄を承認した別ライフサイクルであり、Talosのreset/installer機構へ委譲してから新しいMachineへclaimする。
- 通常のallocationやupdate、deleteのフォールバックとして暗黙に実行してはならない。
- **自動Reprovisionの前提要件**: installed OSが存在する状態からremoteにmaintenance environmentへ戻せるboot strategyをHostが持つ必要がある。Fresh machineのnetwork boot capabilityだけでは自動Reprovisionを許可しない。

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
| **Talos machine configuration** | Talos APIでapply | effective configurationのdiffを評価し、安全な場合のみ適用 |
| **Kubernetes version** | Talos cluster-wide upgrade | Control Plane Providerがcluster-wideにsequenceする |
| **Host identity / 破壊的disk変更** | 自動更新不可 | `UnsafeChange` として安全停止 |

### Update Extensionによる保護

- 初回provisioning後のmutableなTalos OS/config変更を実行できるのはUpdate Extension（Runtime Extension）のみとする。
- `CanUpdateMachineSet` / `CanUpdateMachine` は、old/new双方のSecretを解決して完全なdiffを評価し、安全な差分のみを `Success` + patch で許可する。危険・未知・部分的な差分は `Failure` で確実にvetoする。
- 詳細な未実装タスクは[実装タスク一覧 (タスク1)](tasks.md)を参照。

### Workerの標準Rollout Profile

- Workerの対応するCAPI `RollingUpdate` 設定では、`maxSurge: 0`、`maxUnavailable: 1` を推奨する。
- `maxSurge: 0` により追加Hostを必要とせず、既存Machineを1台ずつin-place updateできる。`maxUnavailable: 0` ではCAPIがバッファのためsurge Machineを作成してしまい追加Hostが必要になるため、ローカルHostを保護する既定値としない。
- `OnDelete` strategyは自動worker in-place update lifecycleとしてサポートしない。
- Control PlaneはMachineDeploymentへ委譲せず、常に1台ずつ更新してetcd、API、Node healthを確認する。

### Kubernetes Upgradeの収束規則

Talosの `upgrade-k8s` はcluster-wide operationであるため、以下の規則でCAPIのdesired stateと収束させる。

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

- **BMC / VM backend**: out-of-bandで電源状態（`PowerOff`）を確認する。
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
