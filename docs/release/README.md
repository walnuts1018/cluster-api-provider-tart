# Release Matrix

このディレクトリでは、repository 内で公開する release candidate の対応範囲を管理する。
機械可読な正本は [release-matrix.yaml](release-matrix.yaml) とし、人間向けの説明と
既知制約は [../release-notes/unreleased.md](../release-notes/unreleased.md) に集約する。

## 現在の公開状態

`release-matrix.yaml` に定義した現行 release candidate の公開状態は次のとおり。

| 名前 | 区分 | 状態 | 参照 |
|---|---|---|---|
| Ubuntu 24.04 amd64 UEFI kubeadm 初期Provisioning | `initialProvisioning` | Supported | [Task 07 証跡](../redesign/runbooks/07-initial-provisioning-simulated-record.md)、[Task 10 証跡](../redesign/runbooks/10-redfish-simulated-record.md) |
| Ubuntu 24.04 amd64 UEFI kubeadm Kubernetes v1.36 初期Provisioning | `initialProvisioning` | Planned | Task 11 |
| Ubuntu 26.04 amd64 UEFI kubeadm Kubernetes v1.36 初期Provisioning | `initialProvisioning` | Planned | Task 11 |
| Debian 13 amd64 UEFI kubeadm Kubernetes v1.36 初期Provisioning | `initialProvisioning` | Planned | Task 11 |
| Ubuntu 24.04 amd64 UEFI k3s Kubernetes v1.36 初期Provisioning | `initialProvisioning` | Planned | Task 11 |
| Ubuntu 26.04 amd64 UEFI k3s Kubernetes v1.36 初期Provisioning | `initialProvisioning` | Planned | Task 11 |
| Debian 13 amd64 UEFI k3s Kubernetes v1.36 初期Provisioning | `initialProvisioning` | Planned | Task 11 |
| Ubuntu 24.04 amd64 UEFI k0s Kubernetes v1.36 初期Provisioning | `initialProvisioning` | Planned | Task 11 |
| Ubuntu 26.04 amd64 UEFI k0s Kubernetes v1.36 初期Provisioning | `initialProvisioning` | Planned | Task 11 |
| Debian 13 amd64 UEFI k0s Kubernetes v1.36 初期Provisioning | `initialProvisioning` | Planned | Task 11 |
| worker OSOnly 更新 | `update` | Experimental | [Task 09 simulated record](../redesign/runbooks/09-kubernetes-lifecycle-simulated-record.md)、[Task 09 recovery](../redesign/runbooks/09-kubernetes-lifecycle-recovery.md) |
| worker KubernetesBinary 更新 | `update` | Experimental | [Task 09 simulated record](../redesign/runbooks/09-kubernetes-lifecycle-simulated-record.md)、[Task 09 recovery](../redesign/runbooks/09-kubernetes-lifecycle-recovery.md) |
| 3 台以上の control plane KubernetesBinary 更新 | `update` | Experimental | [Task 09 simulated record](../redesign/runbooks/09-kubernetes-lifecycle-simulated-record.md)、[Task 09 recovery](../redesign/runbooks/09-kubernetes-lifecycle-recovery.md) |
| 単一 control plane KubernetesBinary 更新 | `update` | Experimental | [Task 09 simulated record](../redesign/runbooks/09-kubernetes-lifecycle-simulated-record.md)、[Task 09 recovery](../redesign/runbooks/09-kubernetes-lifecycle-recovery.md) |

## 読み方

- `Supported` は、現行 release candidate で repository 内に公開する対象を示す。
- `Experimental` は、feature gate または既知制約付きで公開する対象を示す。
- `Planned` は、Task と公開判定条件は定義済みだが、利用者向け機能として公開しない対象を示す。
- 各行の詳細な根拠、GitHub Actions の実行記録、手動復旧手順は `evidencePaths` の参照先に置く。
- 昇格や降格を行うときは、`release-matrix.yaml` と release note を同じ変更で更新する。
