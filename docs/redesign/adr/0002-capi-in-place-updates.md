# ADR 0002: インプレース更新はCAPI Runtime SDK Hooksから調整する

- Status: Proposed
- Date: 2026-06-28

## Context

CAPIの通常更新はMachineを置換する。物理ホスト数に余裕がない環境や単一ノードでは置換用hostを確保できず、A/B slotを持つ同一hostの更新が必要になる。ただしInfrastructure Providerが独自にMachineのバージョン変更を監視して更新すると、MachineDeployment/KubeadmControlPlaneのdrain、順序、quorum管理と競合する。

## Decision

- 明示的にInPlace policyが選択された更新だけをRuntime SDKの`CanUpdateMachine`、`CanUpdateMachineSet`、`UpdateMachine`で処理する。
- `CanUpdate*`はProviderが安全に処理できるspec差分だけをpatchで覆う。
- `UpdateMachine`は長時間処理を同期実行せず、永続operationを開始・再開し、進行中は`retryAfterSeconds`を返す。
- 初期リリースではOS artifactと、そのartifactに含まれる対応済みKubernetes patch/minor updateだけを対象にする。
- version skew、control-plane update順、drain/maintenanceはCAPI側の調整に従う。
- hookが無効または対象外の差分では通常のMachine置換へフォールバックする。
- CAPI v1.13.1ではAlphaかつ既定無効であるため、同一Node更新をfeature gate配下のexperimental機能とし、delete-first host reuseを安定経路として残す。

## Acceptance gate

次をTask 01で実証するまでAcceptedにしない。

1. 使用するCAPIバージョンでKCPとMachineDeploymentの両方から期待したhookが呼ばれる。
2. controller/extension再起動後に同じoperationを再開できる。
3. hook timeout時に通常Reconcileや既存clusterを停止させない。
4. 単一ノードcontrol planeの更新前後でetcdとNode identityを保持できる。

## Consequences

- CAPIのrollout調整を再利用できる。
- Runtime SDKのAlpha APIと対象CAPIバージョンへ依存する。
- Bootstrap Provider側の変更をInfrastructure Providerだけで処理できない場合があり、更新可能範囲を狭く宣言する必要がある。

## Alternatives

- TartMachine controllerだけで任意更新を開始する: CAPIの所有するrolloutと競合するため却下。
- 常にMachineを置換する: host枯渇と単一ノード要件を満たせないため既定fallbackとしてのみ維持。

## References

- [Implementing In-Place Update Hooks Extensions](https://main.cluster-api.sigs.k8s.io/tasks/experimental-features/runtime-sdk/implement-in-place-update-hooks)
