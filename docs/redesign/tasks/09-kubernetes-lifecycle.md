# Task 09: Kubernetes Distribution Lifecycle

## 目的

A/B OS slot切替とは別に、既存node上でkubeadm/k3sの状態変更を安全に実行する責務を実装する。

## 依存

- Task 08のOS-only A/B更新
- ADR 0008

## 実装範囲

- typedな`DistributionLifecycleDriver`
- 署名済みone-shot update planを実行するnode-local service
- kubeadmのpreflight、etcd snapshot、`kubeadm upgrade plan/apply/node`、health確認
- CAPI rollout owner（control planeはKCP、workerはMachineDeployment）が所有するversionとnode順序への追従
- operation credential、plan digest、target version、desired object digest、snapshotRef、step marker、deadline、再起動後再開
- OS-only、binary-only、State migrationの分類
- k3s adapterに必要なBootstrap/Control Plane Provider契約の設計

node-local serviceは任意shell APIや長期管理credentialを持たず、対応version範囲のtyped operationだけを実行する。

workerはcontrol planeがtarget versionを受理した後にslotをstageし、新slotのkubeletを開始する前に`kubeadm upgrade node`を実行する。control planeは旧slot稼働中にpreflightとsnapshotを完了し、target kubeadmで`upgrade apply`を実行してから、新slotを試行起動する。kubeletはState/Data mountと該当lifecycle stepが完了するまで起動しない。

## 受け入れ条件

1. Distribution Lifecycle未対応時はKubernetes version差分を`CanUpdate*` patchで覆わない。
2. worker、複数control-plane、単一ノードの順に有効化し、CAPI rollout ownerの更新順を迂回しない。
3. `kubeadm upgrade`のminor version skipとversion skew違反をpreflightで拒否する。
4. control-plane/etcd migration前にsnapshotと復元可能性を確認する。
5. 同じoperationの再実行でupgrade commandやmigrationを重複適用しない。
6. OS slot rollbackだけではStateが戻らない更新を自動rollback成功として報告しない。
7. update後にNode Ready、control-plane component、etcd quorumを確認する。
8. 単一ノードはmanagement API停止中の復帰を含むE2Eが通るまでexperimentalとする。
9. `TartHostOperation`にplan/desired object digest、target distribution version、lifecycle phase、snapshotRef、完了stepを保存する。
10. preflight、snapshot、apply、slot boot、verifyの各phase直後にcontrollerまたはnodeを再起動しても、重複実行せず再開する。

## 対象外

- 任意versionのpackage manager upgrade
- 任意commandを配るremote execution framework
- storage application dataの自動snapshot

## 関連

- ADR 0002、0008
