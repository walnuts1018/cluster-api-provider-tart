# アーキテクチャ

この文書は、Tartの設計判断を実装へ落とし込むための全体アーキテクチャ方針、Provider責務境界、パッケージ構成を定義する。

---

## 責務の全体像

```text
                    Cluster API
                         |
       +-----------------+-----------------+
       |                 |                 |
       v                 v                 v
 Infrastructure      Bootstrap        Control Plane
    Provider          Provider          Provider
       |                 |                 |
       +-----------------+-----------------+
                         |
                    Tart resources
                    /      |      \
                   v       v       v
                Host     Talos   Kubernetes
              lifecycle   API       API
```

### Provider責務境界

| Provider | 所有する責務 | 所有しない責務 |
| --- | --- | --- |
| **Infrastructure** | `TartHost`のinventory管理、Host allocation、電源制御、ProviderID同期、Talos OS delivery | Talos configurationの生成、cluster secretの生成、初回provisioning後のmutableなTalos OS/config update、CNI/CSIなどのadd-on |
| **Bootstrap** | Talos machine configurationの生成、cluster secret bundleのread-only参照、Bootstrap Secret生成 | cluster secret bundleの生成・更新、OS installation、Host電源制御、etcd membership、kubeconfig |
| **Control Plane** | control-plane Machine群の管理、cluster secret bundleの生成・ローテーション、初回etcd bootstrap、Kubernetesライフサイクル、workload kubeconfig Secret | Host inventory、disk writer、Kubernetes add-on |

Talos Linuxが公式に提供する機能（OSインストーラ、machine configuration、storage/volume、upgrade/rollback、etcd bootstrap、Kubernetes runtime）はTalosへ委譲し、Tart側で再実装しない。

---

## パッケージ構成と依存関係

ルート直下の `internal` および `pkg` ディレクトリは作成しない。
依存方向は以下を基本とする。

```text
cmd/controller-manager
        |
        +--> controller --> api/*/v1alpha1
        |       |
        |       +--> host / talos / bootstrap / controlplane
        |       +--> extensions
        |
        +--> boot / talosの具体的adapter

cmd/netboot-server (controller-managerとは別process)
        |
        +--> netboot
```

- **API層 (`api/`)**: pureな型定義とCRDメタデータのみを配置。
- **ドメイン/ポリシー層 (`host/`, `bootstrap/`, `controlplane/`, `domain/`)**: 純粋なビジネスロジックや計算（CAS判定、digest計算、quorum計算など）。Kubernetes clientに直接依存しない。
- **外部アダプター層 (`talos/`, `boot/`)**: Talos gRPCクライアントや電源制御（WoL/Redfish）などの外部副作用をカプセル化。
- **コントローラー層 (`controller/`)**: Kubernetes resourceのwatch/reconcile。ポリシー層とアダプター層を組み合わせて状態を収束させる。
- **拡張層 (`extensions/`)**: CAPI Runtime Extensionのエンドポイント。
- **network boot層 (`netboot/`, `cmd/netboot-server`)**: ProxyDHCP、TFTP、iPXEスクリプト配信を提供する独立アダプター。Kubernetes clientやcontroller層に依存せず、controller-managerとは別binary・別processとして動作する。まっさらなhostをTalos maintenance modeへ到達させるためだけに使い、TartHost/TartMachineのResource modelには組み込まない。

---

## 副作用境界の定義

1. **Kubernetes API**:
   - Resourceの取得、list、watch、server-side apply、Status patch、Event、Secretの管理を担当する。純粋なポリシーパッケージ（`host/`, `bootstrap/` 等）はcontroller-runtime clientを直接呼び出さない。
2. **Talos API**:
   - maintenance modeでのhardware discovery、authenticated APIでのversion/health観測、configuration apply、OS install、OS upgrade、shutdown、初回bootstrapを担当する。ブロックデバイスへの直接書き込みや独自updaterは行わない。
3. **Host Lifecycle**:
   - Hostのアロケーション、`consumerRef` のatomic CAS、Enrollment/Discoveryのmaintenance boot、電源制御、shutdown確認を担当する。Wake-on-LAN、Redfish、VM API等はバックエンドの差異であり、上位のidentityやライフサイクルセマンティクスを変えない。
4. **Runtime Extension (In-Place Update)**:
   - CAPIのin-place update hookを受け、変更が安全にin-place適用可能かを判定・実行する。初回provisioning後のmutableなTalos更新を実行できるのはUpdate Extensionのみとし、通常コントローラーは観測とStatus反映に留める。

---

## Reconcileの原則

ReconcileはResource Statusを手順番号やステートマシンのステップとして扱わず、常に以下の観測値から毎回次の安全なアクションを判定する。

```text
Kubernetes desired state
  + TartHost spec/statusとinventory
  + Talos API observed version/configuration/health
  + workload cluster observed state
      ↓
次に行うべき安全な副作用、またはConditionの更新
```

1. **冪等性と再入可能性**: 副作用の途中でコントローラーが再起動しても、次回reconcileで外部状態（Talos API、Host状態、Secretなど）を再観測して安全に継続できるようにする。
2. **Server-Side Apply**: Resourceの作成・更新には原則としてServer-Side Apply（SSA）を使用する。ただし、`TartHost.spec.consumerRef` のみは排他制御のためatomic CAS（resourceVersion付きUpdateまたはJSON Patch）で更新する。
3. **安全停止（Fail-Closed）**: 安全にin-place updateできない変更や競合を検知した場合、Machine replacementへの暗黙のフォールバックを行わず、`Ready=False` と安全なReasonで停止する。

---

## 関連ドキュメント

- CAPI Contractおよび不変条件: [API contract](api-contract.md)
- リソースの状態遷移と安全規則: [Machine lifecycle](lifecycle.md)
- 未実装・仮実装タスクの詳細: [実装タスク一覧](tasks.md)
- Talos連携と委譲方針: [Talos連携](talos.md)
- セキュリティ・観測性規約: [セキュリティと観測性](security.md)
