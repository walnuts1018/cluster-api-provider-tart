# セキュリティと観測性

この文書は、Tartにおけるセキュリティ境界、Trust model、機密情報の取り扱い規則、RBAC権限分離、および観測性（Log/Event/Metrics/Tracing/Conditions）の規約を定義する。

---

## Trust Boundary と セキュリティ前提

- **ネットワークの信頼前提**: 一般的なPC環境ではTPM attestationやBMC暗号学的アイデンティティを利用できない場合がある。そのため、初回provisioning network自体をtrusted infrastructure（active attackerから保護されたネットワーク）として明示的なセキュリティ境界に含める。
- **誤接続防止の照合**: MAC、system UUID、インベントリ情報は暗号学的認証ではなく誤接続・誤適用防止の照合材料として扱い、期待したHostとmaintenance endpointの論理的なbindingを必須とする。
- **暗号化と認証の分離**: maintenance Talos APIの自己署名TLSは通信の暗号化に過ぎず、ノードの相互認証ではない。構成適用後に相互TLSによる認証済みTalos APIへ移行する。
- **秘密情報フリーのブート資産**: ファームウェアのブートプロトコルや公開HTTPへクラスタ認証情報を埋め込まず、初期ブート資産はsecret-freeとする。

---

## 機密情報の保護原則

以下の機密情報は、CR Status、Condition message、Event、通常log、metrics label、debug artifactへ絶対に出力してはならない。

- Talos machine secrets / client private key
- Kubernetes PKI private key / CA private key
- Bootstrap Data / Secret raw content
- workload kubeconfig / token / credentials
- BMC password / IPMI credentials
- その他の一切の署名鍵および機密データ

Statusへ公開してよいのは、Secret参照名、生成フラグ、および機密値をredactしたcanonical semantic representationのSHA-256 digestのみである。

---

## Secret のライフサイクル

### 1. Cluster Secret Bundle
- `TartCluster.spec.id` を含む generation 単位で作成されるimmutableなSecret（`<cluster-name>-talos-secrets-<cluster-id>-g<generation>`）。
- Control Plane Providerが作成し、Bootstrap Providerはread-onlyで参照する。
- **CA Rotation**: generation Nを基に更新対象CAのみを変更した generation N+1 の `Pending` Secretを先に永続化し、Talos公式の段階的更新を経てから新generationをactiveにする。過去generationはCluster存続中にGCしない。実装タスクは[実装タスク一覧 (タスク2)](tasks.md)を参照。

### 2. Bootstrap Secret
- type: `cluster.x-k8s.io/secret`
- 単一の `value` keyに完全なTalos machine configurationを格納する。
- 一度生成されたBootstrap Secretは変更せず、構成変更はUpdate Extension（Runtime Extension）に委譲する。

### 3. Talos Recovery Secret
- provider管理namespace上のimmutableなSecret（`tart-talos-recovery-<cluster-id>-<ca-fingerprint>`）であり、`os-ca.crt`、`os-ca.key`、`clusterID`のみを保持する。
- **最小限のmaterial**: Talos API（machine/OS）CAのsigning materialだけを保持し、Kubernetes PKI、service account key、bootstrap token、Bootstrap Data全体は複製しない。
- **短命credential**: 長寿命のadmin client certificateは保存しない。Reset等の操作が必要になった時点でrecovery CAから`os:admin` roleの短命なclient certificate（既定10分）を都度発行し、メモリ内だけで使用する。
- **GCしない条件**: `TartCluster`やMachineのOwnerReferenceでGCしない。少なくとも1台のTartHostが`status.currentTalosIdentityRef`でそのSecretを参照する間は保持し、最後のHostがReprovisionを完了するかinventoryからforgetされた後にだけ削除する。削除可否は参照countではなく、reconcileごとに現在のTartHost集合を観測して判定する。
- **共有単位**: 同一cluster identityかつ同一CA generationに属するHostは1つのSecretを共有し、HostごとにCA private keyを複製しない。

### 4. Immutable な設定入力 (`configSecretRef`)
- ユーザーのraw configuration patchは全て `configSecretRef` 経由で取得し、CRD Specへのinline保存は行わない。
- 参照先Secretは `immutable: true` を必須とし、内容変更時は新しいSecret名への参照更新とする。

---

## 最小権限とRBAC境界

1. **Provider間の権限分離**:
   - Infrastructure、Bootstrap、Control Planeの各Providerは、自身の責務に必要な最小限の権限のみを持つ。
   - CAPI coreがprovider resourceを参照するために必要なaggregated RBACのみを公開する。
2. **管理者フィールドの保護**:
   - `TartHost` の `spec.consumerRef`、`spec.reuseApproval`、`spec.reuseMode`、`spec.forgetApproval` は、Host上のデータ破壊や意図しない再利用につながる重要フィールドである。`spec.reuseMode: Reprovision` と一致する `spec.reuseApproval` の組み合わせは、recovery identityで認証したTalos Resetを許可する唯一の入力である。
   - Kubernetes RBACにはfield-level permissionが存在しないため、これらのSpecを更新できるRoleとcontrollerの権限を分離し、通常のworkloadオペレーターには付与しない（インフラ管理者に限定）。
3. **TartHost直接削除（Forget）の安全性**:
   - Claim中またはRetainedのHostは、現在のbindingまたはretained recordに一致する `spec.forgetApproval` なしに直接削除できない。
   - forget承認後もpower off、Talos reset、disk wipeは実行せず、Tartのinventoryからのみ除外する（物理データの保持）。

---

## Disaster Recovery (DR) におけるセキュリティ境界

- management clusterのバックアップには、`TartHost.spec.id`、`TartCluster.spec.id`、ハードウェアidentity、`consumerRef`、`retainedFrom`、全secret bundle generation、電源設定を同一整合点から含める。
- 復元時は `tart.cluster.x-k8s.io/restore-approved: "true"` annotationと管理者権限を要求し、既存の永続IDを保持する。
- 通常CREATEでpresetされたIDは拒否し、同名Clusterの再作成では新しいCluster IDを要求する。
- `clusterctl move` でこの復元契約を代用しない。

---

## 観測性（Observability）

### 1. Conditions
- 各リソースのCondition typeは固定とし、内部処理の進行ステップ番号や手順名をCondition typeとして乱立させない。
- 安全停止（Fail-Closed）は汎用的な `Blocked` ではなく、`Ready=False` または `Available=False` と具体的な `Reason` で表現する。

### 2. Logging
- 構造化ログ（Structured logging）を使用し、reconcile対象、原因、結果、エラー分類を追跡可能にする。
- 機密情報や高カーディナリティな生データをログに出力しない。

### 3. Events
- ユーザーの確認や介入が必要な重要なlifecycle遷移のみに限定してEventを発行する。
- reconcileの再試行ループごとに重複してEventを発行しない。

### 4. Metrics と OpenTelemetry
- TracerおよびMeterは標準の `otel.GetTracerProvider()` / `otel.GetMeterProvider()` から取得する。
- メトリクスのラベルに機密情報や高カーディナリティな値（ハードウェア詳細情報や動的UIDなど）を含めない。
