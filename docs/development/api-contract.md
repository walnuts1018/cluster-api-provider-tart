# API contract

TartのAPI schemaとCluster API contractの対応、およびProviderが維持すべき不変条件を定義する。
Tart独自APIは`v1alpha1`へリセットし、CAPI coreの現行`v1beta2` contractへ接続する。過去のTart `v1beta1`とのconversionや互換層は作らない。

実装済みの詳細なGo型定義については、[`api/`](../../api)配下の型定義コードを参照すること。

---

## API group

CAPIのproviderごとの責務に合わせてAPI groupを分ける。別groupのprovider resourceをCAPI coreが参照できるよう、生成するCRD、RBAC、aggregated roleに必要な権限を含める。

| API Group | Version | 主なResource | 参照先コード |
| --- | --- | --- | --- |
| `infrastructure.cluster.x-k8s.io` | `v1alpha1` | `TartHost` (cluster-scoped)<br>`TartCluster`, `TartMachine`<br>各Template | [`api/infrastructure/v1alpha1/`](../../api/infrastructure/v1alpha1) |
| `bootstrap.cluster.x-k8s.io` | `v1alpha1` | `TartBootstrapConfig`<br>`TartBootstrapConfigTemplate` | [`api/bootstrap/v1alpha1/`](../../api/bootstrap/v1alpha1) |
| `controlplane.cluster.x-k8s.io` | `v1alpha1` | `TartControlPlane`<br>`TartControlPlaneTemplate` | [`api/controlplane/v1alpha1/`](../../api/controlplane/v1alpha1) |

ここでの `v1alpha1` はTart独自APIのversionであり、CAPI coreの `v1beta2` を意味しない。CAPI contractへ参加するprovider CRDには、contract version labelとして `cluster.x-k8s.io/v1beta2: v1alpha1` を付与する。

---

## Providerが維持する不変条件（Invariants）

### 1. 永続IdentityとSpec IDの管理
- **`TartHost.spec.id`**:
  - Kubernetes metadata UIDとは独立したimmutableな永続ランダムUUID。
  - concreteな `TartHost` のnon-dry-run CREATE後にprovider controllerが一度だけ生成して永続化する。TemplateやSSA dry-runのdefaultingでは生成しない。通常CREATEで利用者が指定することは許可しない。
  - DR復元時のみ、`tart.cluster.x-k8s.io/restore-approved: "true"` annotationと管理者権限がある場合に限りバックアップ済みの値を保持する。
- **`TartCluster.spec.id`**:
  - CAPI `Cluster.metadata.uid` とは独立したimmutableなworkload cluster永続identity。
  - 同名Clusterの再作成では新しいCluster IDを発行し、古いClusterのbundleやHostを再利用しない。
  - このIDが確定するまで、bundle生成、Host claim、provisioningを開始しない。

### 2. ProviderIDの決定論的生成規則
- ProviderIDはHost allocation後に `TartHost.spec.id` から決定論的に生成する。
  ```text
  tart://host/<TartHost.spec.id>
  ```
- Infrastructure ProviderとBootstrap Providerは同じ生成規則を共有し、Talos kubelet（`--provider-id`）およびKubernetes Nodeの `spec.providerID` と完全に一致させる。
- management cluster復元でmetadata UIDが変わっても、`TartHost.spec.id` から同じProviderIDを再構築できる。

### 3. AllocationとClaimの排他性
- `TartHost.spec.consumerRef` をallocation bindingの正本とし、`status` をlockに使わない。
- claimはSSAのfield ownershipではなく、resourceVersion付きUpdateまたはJSON Patchの `test` によるatomic CASで確立する。
- 状態遷移（Available, Claimed, Retained, Reusable）の詳細は [Machine lifecycle](lifecycle.md) を参照。

### 4. Failure Domainの接続契約
- TartClusterがfailure domainsをsurfaceする場合、`TartHost.spec.failureDomain` から `TartCluster.status.failureDomains`、CAPI `Machine.spec.failureDomain`、Host allocatorまで同じ値を接続し、対応するMachineを必ず一致するHostへ割り当てる。
- failure domainをallocationまで接続できない段階では、Statusへ部分的なfailure domainをsurfaceしない。

### 5. ClusterClassとSSA dry-run
- ClusterClassをサポートする場合、Topology controllerがInfraMachineTemplateとBootstrapConfigTemplateへ行うSSA dry-runをprovider webhookが受け入れなければならない。
- dry-runではSecret、OwnerReference、Status、外部API副作用を作成せず、observed stateを前提にした検証や生成済みmetadataを要求しない。
- defaultingとvalidationはdry-runと実適用で同じ結果にし、templateから通常のCAPI resourceへ変換できるfieldだけを検証する。webhookの副作用が必要な入力はClusterClassの完成条件に含めず、実適用時のreconcileへ委譲する。

### 6. Control Plane Provider Contract
- **CAPI v1beta2整合**:
  - `status.versions`、`status.selector`、`status.replicas`、`status.readyReplicas`、`status.availableReplicas`、`status.upToDateReplicas` を持ち、`spec.replicas`、`status.replicas`、`status.selector` を接続する `scale` subresourceを公開する。
  - `nodeDrainTimeoutSeconds`、`nodeVolumeDetachTimeoutSeconds`、`nodeDeletionTimeoutSeconds` は `spec.machineTemplate.spec.deletion` へ置き、rolloutを起こさずに伝播するfieldと区別する。
  - 各control-plane Machineの `spec.minReadySeconds` と `UpToDate` Conditionを継続的に管理する。
  - `controlPlaneInitialized` はAPI serverがrequestを受け付けられる時点でtrueにし、全Node ReadyやCNI導入を待たない。
- **In-place update時の遷移契約**:
  - Control Plane ProviderがMachineを更新する場合、まず `CanUpdateMachine` を呼ぶ。
  - 成功時はresourceVersionを確認しながらCAPI Machine、TartMachine、TartBootstrapConfigのdesired specを更新し、3つ全てへ `in-place-updates.internal.cluster.x-k8s.io/update-in-progress` annotationを設定した後、Machineへ `UpdateMachine` hook pendingを設定する。
  - この遷移は既存annotation、desired spec、hook pending、generationを観測して再入可能にし、途中停止後も二重実行や部分的なidentity変更を起こさない。

### 7. Secret Contract
- **Bootstrap Secret**:
  - type: `cluster.x-k8s.io/secret`
  - data key: 単一の `value` keyのみを持ち、完全なTalos machine configurationを格納する。
  - `TartBootstrapConfig` のcontroller OwnerReferenceを持つ。初回作成後は書き換えず、構成変更はUpdate Extensionに委譲する。
- **Cluster Secret Bundle**:
  - Clusterごとに generation 単位でimmutableなSecret（`<cluster-name>-talos-secrets-<cluster-id>-g<generation>`）を作成する。
  - active generationは `TartCluster.status.activeSecretGeneration` へ反映する。
  - CA rotationでは、generation N+1 の `Pending` Secretを先に永続化し、Talos APIによる段階的切替が完了してから active generation を昇格させる。詳細は [実装タスク一覧 (タスク2)](tasks.md) を参照。
- **Workload Kubeconfig**:
  - `<cluster-name>-kubeconfig` Secretを生成・維持する。単一の `value` keyを持ち、client certificateの有効期限を観測して更新する。
- **Raw configuration input**:
  - ユーザー定義のraw patchは、機密性の有無に関わらず全て `configSecretRef` のimmutableなSecretから読み込み、CRD Specへのinline保存は行わない。

### 8. MHC remediationの生成前保護
- Tart-managed Machineはローカル永続状態の有無を判定できないため、MHCのdelete-and-recreate remediationを安全な既定値とみなさない。
- MachineSetまたはControl PlaneのMachine templateのmetadataへ、生成前から `cluster.x-k8s.io/skip-remediation: "true"` を設定することを必須とする。
- Tart v1alpha1では自動replacementや同じHostへのguided reprovisionのopt-inは提供しない。

### 9. Runtime Extensionの前提条件
- in-place updateを使用するmanagement clusterでは、CAPIの `RuntimeSDK=true` と `InPlaceUpdates=true` feature gateを有効にする。
- TartのHTTPS endpointを `ExtensionConfig` へ登録し、server certificate、TLS Secret、必要なCA trustを管理する。
- 現行CAPIではin-place update hookへ登録できるextensionは1つに制限されるため、他extensionとの競合を避ける。
