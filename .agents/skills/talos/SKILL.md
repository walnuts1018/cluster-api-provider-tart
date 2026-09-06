---
name: talos
description: Talos Linux API、machine configuration、storage、upgradeの委譲境界を確認する
when_to_use: Talos client、Bootstrap Config、installation、hardware discovery、OS/Kubernetes updateを実装・レビューする時
---

# Talos 連携ガイドライン

Talos Linux APIとの通信、machine configurationの合成、OSインストール/アップグレードの委譲を実装・レビューする際のガイドラインである。
達成すべき要件は[開発者向けドキュメント](../../../docs/development/README.md)を参照すること。

---

## 基本原則

1. **機能の委譲**: OSインストール、パーティショニング、アップグレード、ロールバック、etcd bootstrapはTalos公式機構へ委譲する。
2. **アダプターのカプセル化**: [`talos/`](../../../talos) パッケージ内にgRPCクライアントやTLS認証を隠蔽し、コントローラーへは小さな観測結果（ドメイン型）のみを渡す。
3. **合成順序の遵守**: Base configuration → User-owned raw patch（`configSecretRef` 経由）→ Provider-owned invariant（上書き不可）の順で合成する。

---

## 実装チェックリスト

### 1. Configuration 合成と不変条件
- [ ] ユーザー設定を `configSecretRef` のimmutableなSecretから読み込み、CRD Specへinline保存していないか
- [ ] Provider-owned invariant（ProviderID、cluster endpoint、machine roleなど）がuser patchによって上書きされないよう競合検知しているか
- [ ] 競合検知時に上書きせず `Ready=False`、`Reason=ConfigurationConflict` で安全停止しているか
- [ ] configuration digest算出時に機密情報をredactしているか（[`bootstrap/digest.go`](../../../bootstrap/digest.go)）

### 2. Maintenance API と Trust Model
- [ ] maintenance APIの自己署名TLSに対し、物理MACアドレスとclaimed HostのMACが一致することを確認してからconfigurationを適用しているか
- [ ] 曖昧さや競合がある場合に適用を中止し、`Reason=IdentityConflict` で安全停止しているか

### 3. Update と ロールバック
- [ ] 初回provisioning後のmutableなTalos更新を通常のReconcilerから実行せず、Update Extension（Runtime Extension）へ委譲しているか
- [ ] Talosがロールバックを検知した場合、desired Specを自動追従させず `Reason=RolledBack` で停止しているか
- [ ] drain失敗時の停止許容を `TartCluster.spec.updatePolicy.allowDowntime: true` のみに限定しているか

---

## 参照ドキュメント・コード

- 達成すべき要件: [`docs/development/README.md`](../../../docs/development/README.md)
- Talosアダプターコード: [`talos/`](../../../talos)
- Bootstrap合成コード: [`bootstrap/`](../../../bootstrap)
