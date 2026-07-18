# Release Note: unreleased

## 概要

この文書は、2026-07-17 時点の release candidate を repository 内で公開するための暫定 release note である。
公開状態の正本は [../release/release-matrix.yaml](../release/release-matrix.yaml) とし、
この文書では人間向けの要約、既知制約、参照すべき runbook を示す。

## Supported

- Ubuntu 24.04 amd64 UEFI kubeadm の初期Provisioning

## Experimental

- worker OSOnly 更新
- worker KubernetesBinary 更新
- 3 台以上の control plane KubernetesBinary 更新
- 単一 control plane KubernetesBinary 更新

## Planned

- Ubuntu 24.04 amd64 UEFI kubeadm Kubernetes v1.36 初期Provisioning
- Ubuntu 26.04 amd64 UEFI kubeadm Kubernetes v1.36 初期Provisioning
- Debian 13 amd64 UEFI kubeadm Kubernetes v1.36 初期Provisioning
- Ubuntu 24.04 amd64 UEFI k3s Kubernetes v1.36 初期Provisioning
- Ubuntu 26.04 amd64 UEFI k3s Kubernetes v1.36 初期Provisioning
- Debian 13 amd64 UEFI k3s Kubernetes v1.36 初期Provisioning
- Ubuntu 24.04 amd64 UEFI k0s Kubernetes v1.36 初期Provisioning
- Ubuntu 26.04 amd64 UEFI k0s Kubernetes v1.36 初期Provisioning
- Debian 13 amd64 UEFI k0s Kubernetes v1.36 初期Provisioning

## 根拠となる runbook と証跡

- 初期Provisioning
  - [Task 07: Initial Provisioning Simulated Record](../redesign/runbooks/07-initial-provisioning-simulated-record.md)
  - [Task 10: Redfish Simulated Record](../redesign/runbooks/10-redfish-simulated-record.md)
- 更新系
  - [Task 09: Kubernetes Distribution Lifecycle Simulated Record](../redesign/runbooks/09-kubernetes-lifecycle-simulated-record.md)
  - [Task 09: Kubernetes Distribution Lifecycle Recovery Runbook](../redesign/runbooks/09-kubernetes-lifecycle-recovery.md)

Task 07 の runbook には、GitHub Actions `E2E Provisioning Test` の成功 run と
`mise run test-provisioning-e2e` の実行範囲が記録されている。

## Known Constraints

- single control plane の `KubernetesBinary` 更新は Experimental のままとし、feature gate なしでは受理しない。
- 上記対象は management API outage を含む controller 再接続 E2E が未完了である間、Supported に昇格しない。
- `StateMigration` の自動復旧は未提供であり、`RecoveryRequired` 到達時は Runbook ベースの手動復旧が必要。
- 更新系の公開状態は simulated record と recovery runbook に基づくものであり、継続 E2E の成功蓄積をまだ要求する。
- k3s 用 cluster template は、対応Bootstrap/Control Plane Provider とAPI kindが確定するまで公開しない。
- k0s 用 cluster template は、対応Bootstrap/Control Plane Provider とAPI kindが確定するまで公開しない。
- Kubernetes v1.36 の Planned 行は、OS Artifact、Platform Profile、E2E証跡が揃うまで利用者向け機能として公開しない。

## 更新時のルール

- 公開状態を変更する場合は、[../release/release-matrix.yaml](../release/release-matrix.yaml) を同じ変更で更新する。
- `Supported` へ昇格する行では、対象タスク、`target-state.md`、この release note を同じ変更へ含める。
- 追加の platform、distribution、firmware を公開する場合は、runbook と evidence を先に repository 内へ追加する。
