# リソースとProvisioningの流れ

この文書は、Tartにおける各Resourceの所有関係、Cluster API参照構造、およびProvisioningの全体フローを俯瞰するための要約文書である。詳細な状態遷移やAPI契約、未実装タスクについては各正本ドキュメントを参照すること。

- 詳細なライフサイクルと安全規則: [Machine lifecycle](lifecycle.md)
- API定義とCAPI契約の不変条件: [API contract](api-contract.md)
- 未実装・仮実装機能のタスク詳細: [実装タスク一覧](tasks.md)

---

## Resourceの所有・参照構造

Tartでは、Cluster APIの責務分割に合わせてResourceを分離している。

```text
Cluster API                      Tart Resources
+-------------------+            +---------------------+
| Cluster           | ---------> | TartCluster         | (Namespace-scoped, Cluster identity)
+-------------------+            +---------------------+
  |                                |
  +--> Machine (ControlPlane) ---> | TartControlPlane    | (Namespace-scoped, Control Plane lifecycle)
  |      |                         +---------------------+
  |      |                           | creates
  |      +-------------------------> | TartBootstrapConfig | (Namespace-scoped, Talos config)
  |      +-------------------------> | TartMachine         | (Namespace-scoped, Machine binding)
  |                                    | claims
  +--> Machine (Worker) -------------> +-----------------+
                                       | TartHost        | (Cluster-scoped, Host inventory)
                                       +-----------------+
```

### Resourceの責務概要

実装済みの詳細な型定義やフィールド構造は[`api/`](../../api)配下のGoコードを参照。

| Resource | Scope | 主な責務 | 参照先コード |
| --- | --- | --- | --- |
| `TartHost` | Cluster | 物理/仮想Hostのハードウェアインベントリ、電源制御、allocation状態の管理 | [`api/infrastructure/v1alpha1/tarthost_types.go`](../../api/infrastructure/v1alpha1/tarthost_types.go) |
| `TartCluster` | Namespace | CAPI Clusterに対応するInfrastructure Cluster。永続Cluster IDの保持 | [`api/infrastructure/v1alpha1/tartcluster_types.go`](../../api/infrastructure/v1alpha1/tartcluster_types.go) |
| `TartMachine` | Namespace | CAPI Machineに対応するInfrastructure Machine。HostとのbindingおよびTalos OSインストールの進行 | [`api/infrastructure/v1alpha1/tartmachine_types.go`](../../api/infrastructure/v1alpha1/tartmachine_types.go) |
| `TartBootstrapConfig` | Namespace | CAPI MachineのBootstrap Config。Talos machine configurationの提供とBootstrap Secret生成 | [`api/bootstrap/v1alpha1/tartbootstrapconfig_types.go`](../../api/bootstrap/v1alpha1/tartbootstrapconfig_types.go) |
| `TartControlPlane` | Namespace | CAPI Control Plane contract。control-plane Machine群の管理とetcdライフサイクル | [`api/controlplane/v1alpha1/tartcontrolplane_types.go`](../../api/controlplane/v1alpha1/tartcontrolplane_types.go) |

---

## Provisioningの流れ（概要）

```text
1. CAPI Machine / TartMachine の作成
   ↓
2. Hostの選択とatomic CASによるTartHost.spec.consumerRefのclaim
   （TartHost.spec.idから決定論的にProviderIDを生成）
   ↓
3. Discovery boot（maintenance Talos APIへの接続とハードウェアインベントリ観測）
   ↓
4. Bootstrap Secretの待機と取得（TartBootstrapConfigReconcilerが生成したSecret）
   ↓
5. Talos APIへのmachine configuration適用とTalos installerの実行
   ↓
6. Host再起動と認証済み相互TLS Talos APIへの再接続
   ↓
7. Talos健全性とProviderIDの一致確認、TartMachineのInfrastructureReady確定
```

各段階の安全条件、エラーハンドリング、および未実装部分の対応タスクは[Machine lifecycle](lifecycle.md)および[実装タスク一覧](tasks.md)を参照すること。
