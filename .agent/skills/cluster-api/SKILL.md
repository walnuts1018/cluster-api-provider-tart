---
name: cluster-api
description: Cluster APIのCRDやControllerなどを実装する時のルール
when_to_use: Cluster APIに関連する実装を行う時
---

Cluster APIのCRDやControllerなどを作成する際は、[公式ドキュメント](https://cluster-api.sigs.k8s.io/) をよく読んで、これらの使用に準拠するようにして下さい。

- [Provider Overview](https://cluster-api.sigs.k8s.io/developer/providers/overview)
- [Getting Started](https://cluster-api.sigs.k8s.io/developer/providers/getting-started/overview)
  - [Naming Conventions](https://cluster-api.sigs.k8s.io/developer/providers/getting-started/naming)
  - [Implement API Types](https://cluster-api.sigs.k8s.io/developer/providers/getting-started/implement-api-types)
  - [Webhooks](https://cluster-api.sigs.k8s.io/developer/providers/getting-started/webhooks)
  - [Controllers and Reconciliation](https://cluster-api.sigs.k8s.io/developer/providers/getting-started/controllers-and-reconciliation)
- [Contracts](https://cluster-api.sigs.k8s.io/developer/providers/contracts/overview)
  - [Infrastructure Cluster Contract](https://cluster-api.sigs.k8s.io/developer/providers/contracts/infra-cluster)
  - [Infrastructure Machine Contract](https://cluster-api.sigs.k8s.io/developer/providers/contracts/infra-machine)
  - [Infrastructure MachinePool Contract](https://cluster-api.sigs.k8s.io/developer/providers/contracts/infra-machinepool)
- [Best Practices](https://cluster-api.sigs.k8s.io/developer/providers/best-practices)
- [Security Guidelines](https://cluster-api.sigs.k8s.io/developer/providers/security-guidelines)
