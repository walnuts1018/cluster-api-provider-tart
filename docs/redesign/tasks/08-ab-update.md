# Task 08: OSOnly A/Bインプレース更新

## 目的

Kubernetes version、Bootstrap Data、State/Dataを変更せず、同じMachine/Host/Node identityでOS ArtifactをInactive Slotへ更新し、失敗時に旧slotへ自動Rollbackする。

## 依存

- Task 07
- ADR 0002、0003が`Accepted`

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

## 関連

- ADR 0002、0003
- Issue #143
