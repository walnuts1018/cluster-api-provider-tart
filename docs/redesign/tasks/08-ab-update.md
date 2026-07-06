# Task 08: OSOnly A/Bインプレース更新

## 目的

Kubernetes version、Bootstrap Data、State/Dataを変更せず、同じMachine/Host/Node identityでOS ArtifactをInactive Slotへ更新し、失敗時に旧slotへ自動Rollbackする。

## 依存

- Task 07
- ADR 0002、0003が`Accepted`

### Task 01未完了で先行する範囲

2026-07-06の実装方針として、Task 01のQEMU検証を待たず、ADR 0002、0003の
Decisionが成立するという暫定前提でTask 08のproduction codeを先行実装する。
実証前にADRのStatusを`Accepted`へ変更してはならず、RuntimeSDK/InPlaceUpdatesは
既定無効のExperimental feature gate配下に置く。

先行実装する範囲:

- current/desired objectの差分を列挙し、OSOnlyで許可するfieldだけを受理する純粋な判定
- Runtime Hook requestから既存Operationを再利用する冪等なUpdate Operation開始処理
- Inactive Slotだけを対象にする署名済みUpdate Plan生成
- Boot trial、Health Gate、Commit、Rollback、RecoveryRequiredの状態遷移
- Condition、Event、Metric、Traceから失敗Phaseを識別できる観測情報
- Driver、Agent、boot reportをPort経由で置換できる単体テスト可能な境界

先行実装の制約:

- bootloaderが保持するboot trial回数、dm-verity read-only root、電源断耐性は、
  production codeの単体テスト成功だけで実証済みと扱わない。
- QEMUまたは実機での検証結果が前提と異なる場合は、既存の
  `amd64-uefi-ab/v1`を黙って変更せず、互換性を壊す変更を
  `amd64-uefi-ab/v2`として実装する。
- Task 01の受け入れ条件とTask 08のE2E完了証跡は削除せず、実行されるまで未検証として
  Task文書へ記録する。
- Agentへ任意commandを追加せず、slot書き込み、boot試行、Commit、Rollbackを
  型付きPlan Stepとして表現する。

実装は次の境界で分割する。

1. Domainは差分分類、Inactive Slot選択、更新状態遷移、再試行禁止条件を副作用なしで判定する。
2. Applicationはdesired objectsからPlan Digestを決定し、同じDigestのOperationを再利用する。
3. Runtime Extension AdapterはCAPI request/responseの変換だけを担当する。
4. Kubernetes AdapterはOperation、TartHost、TartMachineの状態を永続化する。
5. Agent/Driver AdapterはPlanに列挙された書き込みとboot操作だけを実行する。

異なるPlan Digestのactive Operation、非OSOnly差分、Active Slotを対象とするPlan、
失敗済みArtifact Generationの同一desired specによる再試行は、diskへ書き込む前に拒否する。
Rollback成功時は旧slotをCommitせず既定boot先へ戻し、Operation=`Failed`、
Host=`Provisioned`、TartMachine Ready=`true`へ収束させる。旧slotのHealth Gateも
成立しない場合はOperationとHostを`RecoveryRequired`へ遷移させる。

## 入力

- current/desired Machine、InfraMachine、BootstrapConfig
- current Active Slot
- target OS Artifact Manifest
- Update Policy `InPlace`

## 成果物

- RuntimeSDK/InPlaceUpdates feature gate設定
- `CanUpdateMachine`/`CanUpdateMachineSet`
- 6種類のpatch field allowlist
- `UpdateMachine`とTartHostOperation連携
- Boot trial、Health Gate、Commit、Rollback
- Update Condition/Event/Metric/Trace

## OSOnly差分規則

許可する差分:

- `TartMachine.spec.image.ref`
- `TartMachine.spec.updatePolicy`

拒否する差分:

- Machine Kubernetes version
- Bootstrap payload digest/format
- Platform Profile
- Host selector
- disk layout/root device hint
- providerID
- deletionPolicy

拒否差分が1つでも存在する場合はpatchで覆わず、通常置換へfallbackさせる。

## 受け入れ条件

1. 6種類のpatchについて、許可fieldだけのcaseをin-placeとして受理する。
2. 拒否fieldを1つずつ変更したcaseをin-placeとして受理しない。
3. 同じ`UpdateMachine` requestを100回呼び、Operationを1つだけ作成する。
4. OS-AからOS-Bへ更新後、Node UID、providerID、machine-id、Kubernetes versionが更新前と一致する。
5. write、verify、boot、mount、Node healthの各失敗caseで旧slotへ戻る。
6. boot失敗3回後、4回目に新slotを選択しない。
7. Rollback成功後、Operation=`Failed`、Host=`Provisioned`、TartMachine Ready=`true`とし、更新失敗Conditionを保持する。
8. 旧slotもHealth Gateを通らない場合は`RecoveryRequired`にする。
9. 失敗Artifact Generationを同じdesired specのまま自動再試行しない。
10. RuntimeSDK/InPlaceUpdates無効時はExtension endpointを登録せず通常置換だけを行う。
11. worker、複数control plane、単一control planeを別feature gateで順に有効化する。

## 完了証跡

- 6 patch allow/deny table test
- 100並列UpdateMachine test
- failure injection 5種のslot/Operation最終状態
- Node UID/providerID/machine-id比較
- feature gate on/off E2E

## 対象外

- Kubernetes version更新
- Bootstrap Data変更
- StateMigration
- Firmware更新

## 実装状況（2026-07-06）

Task 01未検証の前提を維持したまま、I/Oへ依存しない更新判断と安全境界を先行実装した。

実装済み:

- CAPI Machine、v1beta1 TartMachine、BootstrapConfigのcurrent/desired差分を分類し、
  `image.ref`と`updatePolicy`以外を拒否する純粋なOSOnly allowlist
- `CanUpdateMachine`と`CanUpdateMachineSet`のv1beta1対応。拒否差分はpatchなしで返し、
  CAPIの通常置換へfallbackさせる
- desired object、target image digest、Artifact Generationから決定的なOperation IDと
  Desired Objects Digestを生成する処理
- 同じ更新開始要求100回を同じOperation IDへ収束させるapplication test
- Inactive SlotのOS/Verity roleだけを許可するEd25519署名済みUpdate Plan
- ManifestのPlatform Profile、architecture、Kubernetes version、generation、image digestを
  disk書込み前に照合する処理
- write、verify、boot、mount、Node health失敗からRollbackへ進む純粋な状態機械
- boot試行3回上限、Rollback成功時の`Failed`/`Provisioned`/Ready維持、
  旧slot不健全時の`RecoveryRequired`収束
- `UpdateMachine` Hookからlive TartMachine/TartHostを取得し、署名検証済みOCI Artifact
  Manifestを使ってOperationとimmutable Plan Secretを作成するadapter接続
- 保存済みOperationのdeadlineとPlan digestを正本にする、Hook再試行時のPlan再生成
- Operationの進行中、成功、失敗phaseからCAPI Runtime Hook retry responseへの写像
- OS Artifact検証鍵とAgent Plan署名鍵を分離したcontroller起動設定
- Update Operation開始時にHostを`Provisioned`から`Updating`へ移すcontroller接続

未実装・未検証:

- boot trial metadataを操作するDriver adapter
- boot reportとNode healthから状態機械eventを生成し、Operation、Host、TartMachineへPatchする処理
- Condition、Event、Metric、Traceの更新失敗観測情報
- worker、複数control plane、単一control planeの段階的feature gate
- controllerからprivate OCI Registryを参照するcredential設定。現時点のManifest解決は
  digest固定参照と署名検証を必須とし、匿名Registryに限定する
- QEMUまたは実機によるdm-verity、boot trial、電源断、Node identity維持、E2E検証

## 関連

- ADR 0002、0003
- Issue #143
