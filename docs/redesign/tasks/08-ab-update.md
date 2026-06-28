# Task 08: A/Bインプレース更新

## 目的

CAPIの調整下で同じMachine、Host、Node identity、State/Dataを維持したOS/Kubernetes更新とrollbackを実装する。

## 依存

- Task 07
- ADR 0002、0003がAcceptedであること

## 実装範囲

- RuntimeSDK/InPlaceUpdates feature gateとExtensionConfig
- `CanUpdateMachine`、`CanUpdateMachineSet`の厳密な差分判定
- `UpdateMachine`と`TartHostOperation`の接続
- inactive slot書き込み、boot trial、health gate、commit
- boot失敗、Node NotReady、期限切れ時のrollback
- update中Conditions、Events、metrics、traces
- state schemaとKubernetes version skew preflight
- OS-only、binary-only、State migrationの更新分類
- single-node向けmaintenance/backup precondition

更新中も`status.initialization.provisioned`はtrueを維持し、`Updating` Conditionで進行を表す。

rolloutはworker限定で開始し、次に複数control-plane、最後に単一ノードcontrol-planeをexperimentalとして有効化する。前段のfailure injectionとrollback条件を満たすまで次の対象を有効化しない。

## 受け入れ条件

1. 未対応差分をpatchで覆わず、通常置換へフォールバックできる。
2. 同じ`UpdateMachine` requestの反復でoperationが重複しない。
3. OS-AからOS-Bへ更新後、Node UID、証明書、etcd、workload/PV dataが保持される。
4. image破損、boot失敗、kubelet失敗、health deadline超過でOS-Aへ戻る。
5. rollback後に失敗したtarget digestを自動再試行し続けない。
6. control-planeを同時更新せず、CAPIが決めた順序を守る。
7. Runtime Extension無効時、既存clusterのReconcileと通常置換を妨げない。
8. Alpha機能であること、対応CAPI version、feature gate、制約を利用者文書へ明記する。
9. State migrationを伴う更新をsnapshot/recovery planなしに自動rollback可能として受理しない。

## 対象外

- 任意のKubernetes minor version飛び越し
- State schemaの破壊的migration
- rollback不能なfirmware update

## 関連

- ADR 0002、0003
- Issue #143
