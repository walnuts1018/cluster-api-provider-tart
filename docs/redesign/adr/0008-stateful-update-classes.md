# ADR 0008: OS slot切替とState migrationを別トランザクションとして扱う

- Status: Accepted
- Date: 2026-06-28

## Context

旧OS slotへ戻すだけでは、`kubeadm upgrade`が変更した`/etc/kubernetes`、etcd、kubelet state等は元に戻らない。OS rollbackをKubernetes全体のrollbackと呼ぶと、古いbinaryと新しいStateの不整合やetcd破損を招く。

## Decision

更新を次の3分類へ分ける。

1. OS-only compatible update: State schemaを変更せず、slot fallbackで自動rollback可能。
2. Kubernetes binary update without state migration: 公式version skewと成果物の互換範囲内だけ自動rollback可能。
3. control-plane、etcd、不可逆なState migration: snapshot、復元手順、maintenance approvalを別operationとして持たない限り自動rollback不可。

Infrastructure Providerはdistribution固有upgradeを推測しない。KCPはversionとnode順序、CABPKは初期Bootstrap Dataを所有する。既存node上の`kubeadm upgrade plan/apply/node`、snapshot、health確認はversionedなDistribution Lifecycle adapterが実行する。k3sは対応するBootstrap/Control Plane Providerと専用adapterを組み合わせる。

## Consequences

- 「slotが戻ったがclusterは戻らない」失敗を明示できる。
- 成果物manifestにState schemaと対応可能なKubernetes version範囲が必要になる。
- 単一ノードでは外部backupとmanagement plane停止中の復元手段がrelease gateになる。
- 一部の更新はoperator approvalを必要とし、完全自動化できない。

## References

- [Kubernetes version skew policy](https://kubernetes.io/releases/version-skew-policy/)
- [Upgrading kubeadm clusters](https://kubernetes.io/docs/tasks/administer-cluster/kubeadm/kubeadm-upgrade/)
