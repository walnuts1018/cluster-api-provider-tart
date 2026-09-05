---
name: host-lifecycle
description: TartHostのallocation、identity、power、boot、data保持を確認する
when_to_use: TartHost、TartMachineのclaim、hardware discovery、power/boot backend、deletionを実装・レビューする時
---

# Host Lifecycle ガイドライン

`TartHost` の登録、アロケーション、電源制御、ライフサイクル完了、およびデータ保持（Retention）を実装・レビューする際のガイドラインである。
詳細な仕様は [Machine lifecycle](../../../docs/development/lifecycle.md) および [API contract](../../../docs/development/api-contract.md) を参照すること。

---

## 4つのアロケーション状態

Hostの適格性分類（実装は [`host/eligibility.go`](../../../host/eligibility.go)）:
1. **`Available`**: `consumerRef` も `retainedFrom` もなく、自動割り当て可能。
2. **`Claimed`**: `consumerRef` で特定の `TartMachine` に割り当て済み。
3. **`Retained`**: Machine削除後に `retainedFrom` が記録され、データ保護のため自動割り当て停止。
4. **`Reusable`**: `spec.reusePolicy: Reusable`、一致する `spec.reuseApproval.retainedFromUID`、および `spec.reuseMode`（`Adopt` または `Reprovision`）が揃った状態。

---

## 実装チェックリスト

### 1. Identity と Claim
- [ ] `TartHost.spec.id` をTemplateやSSA dry-runで生成せず、non-dry-run CREATE後にコントローラーが一度だけ確定しているか
- [ ] claim処理（[`host/claim.go`](../../../host/claim.go)）でSSAではなく、resourceVersion付きUpdateまたはJSON Patchの `test` によるatomic CASを使用しているか
- [ ] MACアドレスやSystem UUIDの重複を観測した際、関係する全Hostを `Ready=False`、`Reason=IdentityConflict` で安全停止しているか

### 2. 電源制御とDiscovery
- [ ] 電源操作（[`boot/`](../../../boot)）の成功をTalosインストール完了とみなさず、APIの接続性とバージョンを独立して検証しているか
- [ ] Discovery bootをBootstrap Secret待ちにせず、secret-freeに実行しているか
- [ ] Talos maintenance APIの自己署名TLSに対し、物理MACとclaimed HostのMACが一致することを確認してからconfigurationを適用しているか

### 3. Deletion と Retention
- [ ] Machine削除時に、authenticated Talos APIへのshutdown要求と停止確認を行うまでclaimを解除しないようになっているか
- [ ] 停止確認が取れない場合、claimとfinalizerを保持して `ShutdownUnconfirmed` を設定しているか
- [ ] claim解除時に直前のconsumer UIDとCluster IDを `spec.retainedFrom` へ記録し、Hostを `Retained` として保持しているか
- [ ] `TartHost` の直接削除（forget）時に、一致する `spec.forgetApproval` を要求し、物理的なdisk wipeを行わないようになっているか

---

## 参照ドキュメント・コード

- ライフサイクルと状態遷移: [`docs/development/lifecycle.md`](../../../docs/development/lifecycle.md)
- API契約: [`docs/development/api-contract.md`](../../../docs/development/api-contract.md)
- 未実装タスク一覧: [`docs/development/tasks.md`](../../../docs/development/tasks.md)
- 実装コード: [`host/`](../../../host), [`boot/`](../../../boot)
