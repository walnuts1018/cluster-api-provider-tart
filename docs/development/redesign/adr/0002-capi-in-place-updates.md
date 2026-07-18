# ADR 0002: インプレース更新はCAPI Runtime SDK Hooksから調整する

- Status: Proposed
- Date: 2026-06-28

## Context

CAPIの通常更新はMachineを置換する。物理ホスト数に余裕がない環境や単一ノードでは置換用hostを確保できず、A/B slotを持つ同一hostの更新が必要になる。ただしInfrastructure Providerが独自にMachineのバージョン変更を監視して更新すると、MachineDeployment/KubeadmControlPlaneのdrain、順序、quorum管理と競合する。

## Decision

- `TartMachine.spec.updatePolicy.mode=InPlace`の更新だけをRuntime SDKの`CanUpdateMachine`、`CanUpdateMachineSet`、`UpdateMachine`で処理する。
- OSOnlyではOS Artifact参照とUpdate Policyの差分だけをpatchで覆う。Machine version、Bootstrap payload digest、Host selector、Platform Profile、disk設定の差分を覆わない。
- `UpdateMachine`は処理を同期実行せず、`TartHostOperation`を作成または再利用する。
- 非terminal Operationでは`status=Success`、`retryAfterSeconds=10`を返す。`Succeeded`ではretryを0、`Failed/RecoveryRequired`では`status=Failure`を返す。
- 初期リリースではKubernetes versionを変えないOS-only artifact更新だけを対象にする。
- Kubernetes更新はTask 09以降とし、version skew、node順、drain/maintenanceはCAPI rollout ownerの調整に従う。
- hookが無効または対象外の差分では通常のMachine置換へフォールバックする。
- CAPI v1.13.1ではAlphaかつ既定無効であるため、同一Node更新をfeature gate配下のexperimental機能とし、delete-first host reuseを安定経路として残す。

## Acceptance gate

次をTask 01で実証するまでAcceptedにしない。

1. CAPI v1.13.1でKCPから`CanUpdateMachine`、MachineDeploymentから`CanUpdateMachineSet`が呼ばれるintegration testが成功する。
2. Extension再起動後、同じPlan Digestに対して新しいOperationを作らない。
3. Extension endpointを停止した場合、既存Machineの通常Reconcileが継続し、in-place更新だけがtimeoutになる。
4. 単一control planeのOSOnly更新前後でNode UID、providerID、etcd member IDが一致する。

## Consequences

- CAPIのrollout調整を再利用できる。
- Runtime SDKのAlpha APIと対象CAPIバージョンへ依存する。
- Bootstrap Provider側の変更をInfrastructure Providerだけで処理できない場合があり、更新可能範囲を狭く宣言する必要がある。

## Alternatives

- TartMachine controllerだけで任意更新を開始する: CAPIの所有するrolloutと競合するため却下。
- 常にMachineを置換する: host枯渇と単一ノード要件を満たせないため既定fallbackとしてのみ維持。

## References

- [Implementing In-Place Update Hooks Extensions](https://main.cluster-api.sigs.k8s.io/tasks/experimental-features/runtime-sdk/implement-in-place-update-hooks)
- [CAPI v1.13.1 feature gates](https://github.com/kubernetes-sigs/cluster-api/blob/v1.13.1/feature/feature.go)
