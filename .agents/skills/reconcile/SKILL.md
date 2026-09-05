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

Conditionは外部から意味を理解できる能力や状態を表す。[API contract](../../../docs/development/api-contract.md)でResourceごとに定めた`Ready`、`Available`、`InventoryReady`、`Claimed`、`TalosReachable`、`Provisioned`、`UpToDate`と、Control Plane contractで定めたConditionだけを使う。安全停止はCAPI-facing Resourceの`Ready=False`または`Available=False`のreasonで表し、`Writing`、`Verifying`、`Step3`のような内部手順をStatusへ保存しない。

Transientなpower待ち、address待ち、Talos API待ち、reboot、Kubernetes APIの一時的なunavailableは再試行可能なConditionとrequeueで扱う。identity mismatch、destructive change、unsupported update、quorum violation、rollbackは`Ready=False`と具体的なreasonへ反映し、同じ危険な副作用を繰り返さない。

## Ownerとwatch

CAPI Machineとprovider resourceの対応にはOwnerReference、CAPI label、reference fieldを一貫して使う。長寿命の`TartHost`へCAPI MachineのOwnerReferenceを付けない。関連Resourceをwatchする場合は、名前の規則やprocess memoryだけに依存せず、labelとreferenceからenqueue対象を決定する。

## Finalizer

Finalizerを使う場合は、削除時に必要な安全な解放処理だけを担当させる。CAPI Machine controllerがdrainとvolume detachを行い、Control Plane Providerがscale-down用pre-terminate delete hookでetcd member removalを行う。`TartMachine`のfinalizerはauthenticated Talos shutdown、停止確認、`TartHost.spec.retainedFrom`の記録、Host claim解除、`Retained`化を担当する。Talos APIに到達できない、停止を確認できない、またはHostが稼働している場合はclaimとfinalizerを保持して`Ready=False`とreasonを設定する。Cluster全体の削除ではetcd member removalを必須にしない。Cluster存続中はcluster secret bundleの過去generationをGCせず、削除時にManaged Machineのretention、DR保持、Retained Hostの再利用制約を確認した後だけGCを許可する。CA rotationでは次generationのimmutable `Pending` SecretをTalos operation開始前に永続化し、完了観測後にだけactive generationを切り替える。OS再インストール、cleaning、partition変更、disk wipeを開始してはならない。

## 外部API

Talos、power、boot、workload Kubernetes APIのclientはcontrollerの外側のadapterへ閉じ込める。controllerは具体的なgenerated API型ではなく、観測結果と必要な操作の小さなinterfaceへ依存する。副作用前にidentityと対象Hostを再確認し、別Hostへの誤適用をfail closedする。

## Runtime Extension

CAPIの`CanUpdateMachineSet`、`CanUpdateMachine`、`UpdateMachine`を一体で実装する。`CanUpdate*`はold/new双方の`configSecretRef`を解決してeffective Talos configurationをrenderし、Secret参照名ではなくsemantic diff全体を判定する。Secretのmissing、unreadable、generation不明は`unknown`として扱う。desired diff全体をcoverできる安全な差分だけを`Success`と完全なpatchで返し、unsafe、unknown、partial diffはpatchなしの`Failure`として停止する。Tartではこの`Failure`をupdateのvetoとして扱い、CAPI minorごとにMachineSet、Machine、TartHost claimが作られないことをE2Eで確認する。`UpdateMachine`だけがTalos operationを実行し、通常のInfrastructure/Bootstrap reconcileは初回provisioning後のmutable diffを観測してもoperationやBootstrap Secret再生成を開始しない。Control Plane Providerが遷移を開始する場合は`CanUpdateMachine`成功後にMachine、InfraMachine、BootstrapConfigをannotation付きで更新し、Machineへ`UpdateMachine` hook pendingを設定する。この遷移はrace-free、re-entrantに観測から再開できるようにする。`TartMachine`の安全停止、Hostの`Retained` gate、MHCの`cluster.x-k8s.io/skip-remediation`、`maxSurge: 0`/`maxUnavailable: 1`のrollout profileを併用する。

node-disruptiveなTalos operationの前にはNodeをquiesceする。Talos operation自身が安全なdrainを提供する場合はそれを利用し、提供しない場合はworkload cluster側でcordon/drainする。multi-node clusterではdrain成功を必須とし、失敗時は副作用を開始しない。single-node clusterではcordonとgraceful evictionを可能な範囲で試し、`TartCluster.spec.updatePolicy.allowDowntime: true`が明示されている場合だけavailabilityを理由に永久blockせず、persistent data preservationを優先して副作用を開始できる。未指定または`false`なら安全停止する。Talosが旧versionへrollbackした場合はdesired Specを自動で戻さず、`UpdateMachine`を`Failure`、`Reason=RolledBack`として後続のControl Plane updateを停止する。MHCの`cluster.x-k8s.io/skip-remediation`はMachineSetまたはControl PlaneのMachine templateへMachine生成前から設定し、Machine作成後の後追いannotationだけに依存しない。Tart v1alpha1では自動replacementのopt-inを提供しない。

## 再試行

requeue間隔は外部APIの性質に合わせ、無制限のbusy loopを作らない。ネットワーク越しの処理にretryを追加する場合は、timeout、最大試行回数、backoffを明示する。成功したか不明な副作用は、再送前に観測APIで結果を確認する。
