# Task 09: Kubernetes Distribution Lifecycle Recovery Runbook

## 目的

`TartHostOperation.status.phase=RecoveryRequired` で停止した Kubernetes Distribution Lifecycle を、
`status.snapshotRef` に保存された復元対象を根拠に安全に切り分け、復旧可否を判断する。

この Runbook は Task 09 の範囲で実装済みの観測点に基づく。自動復旧や Recovery Operation の
再実行 API はまだ完成していないため、復旧作業自体は手動で行う。

## 適用条件

次のすべてを満たす場合に使用する。

1. 対象 `TartHostOperation` の `status.phase` が `RecoveryRequired` である。
2. 対象 Operation の `spec.type` が `Update` である。
3. 対象 Operation の `spec.updateClass` が `StateMigration` または manual recovery 判断を要する
   `KubernetesBinary` である。

`status.phase=Failed` で旧 slot が健全な場合は、この Runbook ではなく通常の再試行判断を行う。

## 復旧前に確認する情報

### 必須観測点

- `TartHostOperation.status.completedSteps`
- `TartHostOperation.status.lifecyclePhase`
- `TartHostOperation.status.snapshotRef`
- `TartHostOperation.status.lastBootReport`
- `TartMachine.status.activeSlot`
- `TartMachine.status.installedImageDigest`
- `TartMachine.status.installedDistributionVersion`
- `TartHost.status.phase`
- 対象 Node の `Ready`、version、static Pod、etcd quorum、API health

### 収集コマンド例

```bash
kubectl get tarthostoperation -n <namespace> <operation-name> -o yaml
kubectl get tartmachine -n <namespace> <machine-name> -o yaml
kubectl get tarthost -n <namespace> <host-name> -o yaml
kubectl get node <node-name> -o yaml
kubectl describe node <node-name>
```

control plane 更新では、管理クラスタまたは対象 Node から次も保存する。

```bash
kubectl get --raw=/readyz
etcdctl endpoint health --cluster
crictl ps --name kube-apiserver
crictl ps --name kube-controller-manager
crictl ps --name kube-scheduler
```

## completedSteps の解釈

`completedSteps` は次の順序で 1 回だけ増える前提で保存される。

1. `PreflightCompleted`
2. `SnapshotCreated`
3. `TargetSlotWritten`
4. `KubeadmApplied`
5. `TargetSlotBooted`
6. `HealthVerified`
7. `Committed`

`SnapshotCreated` が記録されているのに `status.snapshotRef` が空であれば、Status 永続化の不整合として
扱い、復旧を開始する前に controller 側の不具合調査を優先する。

## 判断フロー

### 1. Snapshot の有無で分岐する

- `spec.updateClass=StateMigration` で `status.snapshotRef` が空:
  実装不整合。復旧を進めず、controller のバグとして扱う。
- `status.snapshotRef` がある:
  復元候補を保持しているため、State 復元を検討する。

### 2. `completedSteps` で破壊範囲を見積もる

- `PreflightCompleted` まで:
  State/Data は未変更の想定。原因を除去後に新しい Update Operation を作り直す。
- `SnapshotCreated` 以降 `KubeadmApplied` 未満:
  Snapshot は取得済み、Kubernetes State 変更は未着手。target slot 書き込み失敗や boot 失敗の
  切り分けを行い、State 復元は通常不要。
- `KubeadmApplied` 以降:
  Kubernetes State が変更済みの可能性がある。旧 slot へ戻しても整合性が保証されないため、
  `status.snapshotRef` を使った復元判断を必須にする。

### 3. Host/Node の到達性で分岐する

- Node へ到達でき、static Pod と etcd が健全:
  まず観測不足を疑い、version や providerID 不一致などの論理失敗を切り分ける。
- Node へ到達できるが etcd または API が不健全:
  Snapshot 復元の第一候補とする。
- Node へ到達できない:
  BMC/コンソール、boot 履歴、`lastBootReport` を使って target slot boot 可否を確認する。

## 復旧手順

### 手順 1. rollout を止める

同じ Host/Machine に対して新しい更新を開始しない。必要に応じて rollout owner 側で対象 Node の
更新を止め、同一 Host 上の再入を防ぐ。

### 手順 2. 証跡を固定する

最低限、次を保存する。

- `TartHostOperation` YAML
- `TartMachine` YAML
- `TartHost` YAML
- Node の状態
- etcd health / API health の結果
- コンソールまたは BMC から確認した boot 失敗情報

### 手順 3. SnapshotRef を検証する

`status.snapshotRef` が指す Snapshot がまだ取得可能で、対象 cluster/host のものか確認する。
Snapshot 名だけで信頼せず、Operation ID、Host、取得時刻、restore test 成功記録を照合する。

### 手順 4. 復元方針を選ぶ

- `KubeadmApplied` 未満:
  原因除去後に新しい Update Operation を作り直す方針を優先する。
- `KubeadmApplied` 以降かつ etcd/API 不健全:
  Snapshot から State を復元する。
- 単一 control plane で management API が停止:
  management API の復帰手段を先に確保する。Task 09 時点では Experimental 扱いのままとする。

### 手順 5. State を復元する

復元の実行主体は Node 側または管理者の復旧環境でよいが、次を必須とする。

1. `status.snapshotRef` と一致する Snapshot だけを使う。
2. 復元対象は更新前の etcd / Kubernetes State とし、別 Operation の Snapshot を混在させない。
3. 復元後に kube-apiserver、controller-manager、scheduler、etcd quorum を再確認する。

### 手順 6. 復元後の整合性を確認する

最低限、次が揃うまで `Succeeded` 扱いにしない。

- Node `Ready=True`
- 期待する Kubernetes version
- control plane static Pod が全部 Ready
- `etcdctl endpoint health --cluster` 成功
- `kubectl get --raw=/readyz` 成功

### 手順 7. 新しい Operation で再開する

`RecoveryRequired` に到達した Operation を再利用して進めない。原因を除去し、必要な Snapshot/Artifact/
Plan を見直した上で新しい Operation を作る。

## 禁止事項

- `status.snapshotRef` が空のまま StateMigration を復旧済みと判断しない。
- `KubeadmApplied` 以降の失敗を、slot 切り戻しだけで `Succeeded` と報告しない。
- `completedSteps` を手で書き換えて再開済み扱いにしない。
- 別 Host、別 Operation、別 cluster の Snapshot を流用しない。
- rollout owner に通知せず複数 Node を同時に手動復旧しない。

## 実行記録テンプレート

PR または検証記録には次を残す。

```text
- Operation:
- Host:
- Machine:
- UpdateClass:
- completedSteps:
- lifecyclePhase:
- snapshotRef:
- failure symptom:
- recovery action:
- post-recovery health:
- next operation decision:
```

## Task 09 時点の制約

- Recovery 自体を自動実行する controller/API は未実装。
- 7 再起動 point の E2E 証跡は未追加。
- 単一 control plane の management API 停止を含む復旧は Experimental のまま。
