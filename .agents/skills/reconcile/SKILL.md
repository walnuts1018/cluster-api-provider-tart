---
name: reconcile
description: TartのKubernetes Reconcileとserver-side applyを実装する
when_to_use: Kubernetes ResourceのReconcile、Status、finalizer、外部副作用を実装・レビューする時
---

# Reconcile実装方針

## 基本形

ReconcileはKubernetes上のdesired stateと、TartHost、Talos API、workload clusterのobserved stateを入力にする。Statusやprocess memoryをworkflowのprogram counterとして使わず、controller再起動後も同じ入力から次のactionを再計算する。

```text
read desired state
  -> read observed state
  -> classify change and error
  -> observe completion before side effect
  -> perform one safe side effect
  -> patch observed state and condition
```

副作用は一回送ったことではなく、外部systemの結果で完了を判定する。Talos upgradeの直後にprocessが停止しても、次回はTalos version、reachability、healthを確認してから再試行する。

## Patch

Resourceの作成と通常のSpec管理はserver-side applyを使う。既存objectを取得して全面的に`Update`または`Create`する実装は禁止する。apply objectにはcontrollerが所有するfieldだけを含め、固定したfield managerを指定する。既存objectのユーザーfieldや別controllerのfieldをforceで奪わない。ただし`TartHost.spec.consumerRef`はSSAをlockとして使わず、resourceVersion付きUpdateまたはJSON Patchの`test`によるatomic CASでclaimする。

StatusはStatus subresourceへserver-side applyまたはpatchする。Condition、observedGeneration、observed valuesだけを変更し、desired SpecをStatusへコピーしない。Statusの更新に失敗した場合はreconcile errorとして返し、秘密情報をerror messageへ含めない。

## Condition

Conditionは外部から意味を理解できる能力や状態を表す。`Ready`、`Claimed`、`TalosReachable`、`Provisioned`、`UpToDate`、`Updating`、`Healthy`、`Blocked`などを使い、`Writing`、`Verifying`、`Step3`のような内部手順をStatusへ保存しない。

Transientなpower待ち、address待ち、Talos API待ち、reboot、Kubernetes APIの一時的なunavailableは再試行可能なConditionとrequeueで扱う。identity mismatch、destructive change、unsupported update、quorum violationは明確なblocked Conditionへ反映し、同じ危険な副作用を繰り返さない。

## Ownerとwatch

CAPI Machineとprovider resourceの対応にはOwnerReference、CAPI label、reference fieldを一貫して使う。長寿命の`TartHost`へCAPI MachineのOwnerReferenceを付けない。関連Resourceをwatchする場合は、名前の規則やprocess memoryだけに依存せず、labelとreferenceからenqueue対象を決定する。

## Finalizer

Finalizerを使う場合は、削除時に必要な安全な解放処理だけを担当させる。CAPI Machine controllerがdrainとvolume detachを行い、Control Plane Providerがscale-down用pre-terminate delete hookでetcd member removalを行う。`TartMachine`のfinalizerはauthenticated Talos shutdown、停止確認、`TartHost.spec.retainedFrom`の記録、Host claim解除、`Retained`化を担当する。Talos APIに到達できない、停止を確認できない、またはHostが稼働している場合はclaimとfinalizerを保持して`Blocked`にする。Cluster全体の削除ではetcd member removalを必須にしない。cluster secret bundleのGCはManaged Machineのretention完了後に行う。OS再インストール、cleaning、partition変更、disk wipeを開始してはならない。

## 外部API

Talos、power、boot、workload Kubernetes APIのclientはcontrollerの外側のadapterへ閉じ込める。controllerは具体的なgenerated API型ではなく、観測結果と必要な操作の小さなinterfaceへ依存する。副作用前にidentityと対象Hostを再確認し、別Hostへの誤適用をfail closedする。

## Runtime Extension

CAPIの`CanUpdateMachineSet`、`CanUpdateMachine`、`UpdateMachine`を一体で実装する。`CanUpdate*`はdesired diff全体をcoverできる安全な差分だけを`Success`と完全なpatchで返し、unsafe、unknown、partial diffはpatchなしの`Failure`として停止する。`UpdateMachine`だけがTalos operationを実行し、通常のInfrastructure/Bootstrap reconcileは初回provisioning後のmutable diffを観測してもoperationやBootstrap Secret再生成を開始しない。Control Plane Providerが遷移を開始する場合は`CanUpdateMachine`成功後にMachine、InfraMachine、BootstrapConfigをannotation付きで更新し、Machineへ`UpdateMachine` hook pendingを設定する。この遷移はrace-free、re-entrantに観測から再開できるようにする。`TartMachine`のblocked判定、Hostの`Retained` gate、MHCの`cluster.x-k8s.io/skip-remediation`、`maxSurge: 0`/`maxUnavailable: 1`のrollout profileを併用する。

## 再試行

requeue間隔は外部APIの性質に合わせ、無制限のbusy loopを作らない。ネットワーク越しの処理にretryを追加する場合は、timeout、最大試行回数、backoffを明示する。成功したか不明な副作用は、再送前に観測APIで結果を確認する。
