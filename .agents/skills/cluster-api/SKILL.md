---
name: cluster-api
description: TartのCluster API Provider contractとResource実装を確認する
when_to_use: CAPI Resource、Provider contract、Controller、ClusterClass、Runtime Extensionを実装・レビューする時
---

# Cluster API実装ガイドライン

Cluster API（CAPI）Providerとしての契約、リソース連携、およびRuntime Extensionの実装・レビューを行う際のガイドラインである。
達成すべき要件は[開発者向けドキュメント](../../../docs/development/README.md)を参照すること。詳細な契約はCAPI公式ドキュメントとコード([`api/`](../../../api))を正本とする。

---

## API Group と Contract Version

- Infrastructure: `infrastructure.cluster.x-k8s.io/v1alpha1`
- Bootstrap: `bootstrap.cluster.x-k8s.io/v1alpha1`
- Control Plane: `controlplane.cluster.x-k8s.io/v1alpha1`
- CAPI contract version label: `cluster.x-k8s.io/v1beta2: v1alpha1` を各CRDに付与する。

---

## 実装チェックリスト

### 1. Spec・Statusと不変条件
- [ ] `TartHost.spec.id` および `TartCluster.spec.id` をTemplateやSSA dry-runで生成していないか（non-dry-run CREATE後にコントローラーが一度だけ確定）
- [ ] ProviderIDが `TartHost.spec.id` から決定論的（`tart://host/<ID>`）に生成され、CAPI InfraMachine、TartMachine、Nodeで一致しているか
- [ ] Statusにworkflowのステップ番号やプログラムカウンタを保存していないか
- [ ] 安全停止時に汎用 `Blocked` ではなく `Ready=False` または `Available=False` と具体的なReasonを設定しているか
- [ ] `controlPlaneInitialized` をAPI server受付開始時点でTrueにしているか（全Node ReadyやCNI導入を待たない）

### 2. Secret Contract
- [ ] Bootstrap Secretがtype `cluster.x-k8s.io/secret`、単一の `value` keyを持ち、完全なTalos machine configurationを格納しているか
- [ ] ユーザーのraw patchをCRD Specへinline保存せず、`configSecretRef` のimmutableなSecretから読み込んでいるか
- [ ] 機密情報（鍵、トークン、未redactの設定）をStatusやEvent、通常ログに出力していないか

### 3. Update と Remediation
- [ ] MHCのdelete-and-recreate remediationを抑止するため、Machine template生成前から `cluster.x-k8s.io/skip-remediation: "true"` を設定しているか
- [ ] Runtime Extension（`CanUpdateMachineSet`, `CanUpdateMachine`）が未知・危険・部分的な差分に対してpatchなしの `Failure` を返し、安全にvetoしているか
- [ ] `allowDowntime: true` が明示されていない限り、drain失敗時に更新を中断（安全停止）しているか

---

## 参照ドキュメント・コード

- 達成すべき要件・非目標: [`docs/development/README.md`](../../../docs/development/README.md)
- Go型定義: [`api/`](../../../api)
