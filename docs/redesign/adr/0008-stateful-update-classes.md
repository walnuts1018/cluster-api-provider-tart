# ADR 0008: OS slot切替とState migrationを別トランザクションとして扱う

- Status: Accepted
- Date: 2026-06-28

## Context

旧OS slotへ戻すだけでは、`kubeadm upgrade`が変更した`/etc/kubernetes`、etcd、kubelet state等は元に戻らない。OS rollbackをKubernetes全体のrollbackと呼ぶと、古いbinaryと新しいStateの不整合やetcd破損を招く。

## Decision

更新を次の3分類へ分ける。

1. `OSOnly`: Kubernetes version、Bootstrap payload digest、stateSchemaを変更しない。自動Rollbackを必須とする。
2. `KubernetesBinary`: Kubernetes versionを変更するが不可逆なState/Data format変更を行わない。検証済みversion pairだけRollbackを許可する。
3. `StateMigration`: etcd、kubeadm、k3s等がState/Data formatを変更する。自動Rollbackを禁止し、SnapshotRefを必須とする。

controllerがPlan作成時に更新クラスを決定し、AgentまたはNode Lifecycle Serviceが推測してはならない。CAPI rollout ownerはversionとnode順、Bootstrap Providerは初期Bootstrap Dataを所有する。既存Node上の`kubeadm upgrade plan/apply/node`、Snapshot、Health GateはversionedなDistribution Lifecycle Adapterが実行する。

## Consequences

- 「slotが戻ったがclusterは戻らない」失敗を明示できる。
- 成果物manifestにState schemaと対応可能なKubernetes version範囲が必要になる。
- 単一ノードでは外部backupとmanagement plane停止中の復元手段がrelease gateになる。
- 一部の更新はoperator approvalを必要とし、完全自動化できない。

## Alternatives

- 全更新を`OSOnly`として旧slotへ戻す: State/Dataの変更を戻せず、古いbinaryと新しいStateの組合せを作るため却下。
- Kubernetes更新を常にMachine置換で行う: 安定経路として維持するが、単一ノードを停止せず同じNode identityで更新する要件を満たさないため唯一の方式にはしない。
- State/Data全体を更新前に複製する: 必要容量と停止時間が大きく、外部PVを含む一貫性を保証できないため共通Rollback方式にはしない。

## References

- [Kubernetes version skew policy](https://kubernetes.io/releases/version-skew-policy/)
- [Upgrading kubeadm clusters](https://kubernetes.io/docs/tasks/administer-cluster/kubeadm/kubeadm-upgrade/)
