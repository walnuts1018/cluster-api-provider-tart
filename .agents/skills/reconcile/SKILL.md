---
name: reconcile
description: TartのKubernetes Reconcileとserver-side applyを実装する
when_to_use: Kubernetes ResourceのReconcile、Status、finalizer、外部副作用を実装・レビューする時
---

# Reconcile 実装ガイドライン

TartコントローラーにおけるReconcileループ、Server-Side Apply（SSA）、Status更新、および外部副作用の扱いに関するガイドラインである。
達成すべき要件は[開発者向けドキュメント](../../../docs/development/README.md)を参照すること。

---

## Reconcileの基本ループ

```text
read desired state
  -> read observed state
  -> classify change and safety
  -> observe completion before side effect
  -> perform one safe side effect
  -> patch observed state and condition
```

- **ステートレスな判断**: Statusやメモリ上のステップ番号をプログラムカウンタとして使わず、外部の観測状態から毎回次のアクションを判断する。
- **副作用の完了判定**: 副作用はAPIリクエストを送信したことではなく、外部システム（Talos API、Host状態など）で完了が観測されたことで完了と判定する。

---

## Patch と Server-Side Apply

1. **Spec の更新**:
   - 原則としてServer-Side Apply（SSA）を使用し、コントローラー固有のfield managerを設定する。
   - 例外: `TartHost.spec.consumerRef` のclaimはSSAではなく、resourceVersion付きUpdateまたはJSON Patchの `test` によるatomic CASで行う。
2. **Status の更新**:
   - Status subresourceに対してパッチまたはSSAを行う。
   - Specの値をそのままStatusへコピーせず、観測された値とConditionのみを更新する。

---

## 実装チェックリスト

- [ ] コントローラー再起動後も、外部状態（Talos API、Host状態、Secretなど）の観測から安全に処理を再開できるか
- [ ] 外部副作用（Talos API呼出し、電源制御）をコントローラー内にべた書きせず、アダプター層に委譲しているか
- [ ] 安全停止時に汎用的な `Blocked` ではなく `Ready=False` または `Available=False` と具体的なReasonを設定しているか
- [ ] Finalizerで安全な解放処理（Talos shutdown、停止確認、`retainedFrom` 記録）を行い、停止未確認のままclaimを解除していないか
- [ ] 一時的なエラー（電源待ち、接続待ち）に対して適切なbackoff付きのrequeueを行っているか

---

## 参照ドキュメント・コード

- 達成すべき要件: [`docs/development/README.md`](../../../docs/development/README.md)
- コントローラーコード: [`controller/`](../../../controller)
