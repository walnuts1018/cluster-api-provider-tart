# 開発ガイド

## 開発環境

必要な Go、Kubebuilder、controller-gen、kustomize、lint ツールは `mise.toml` で管理する。個別に
ツールを導入せず、まず次を実行する。

```bash
mise install
MISE_OFFLINE=1 mise run help
```

このリポジトリでは Go の build cache と module cache をリポジトリ配下へ置く。sandbox や CI と
同じ条件で実行するため、通常の開発では `MISE_OFFLINE=1` を付ける。

ローカルでも実行する処理はmise taskとして公開し、長い実装は `scripts/<task名>/script.sh` に置く。GitHub
Actionsだけで使う公開・Renovate補助は `.github/scripts/` に置き、ローカルtaskには追加しない。

## 日常の確認

Go コードを変更したときは、変更内容に応じて次を実行する。

```bash
MISE_OFFLINE=1 mise run build
MISE_OFFLINE=1 mise run lint
MISE_OFFLINE=1 mise run test
```

`build` と `test` は CRD、RBAC、DeepCopy、Kessoku の生成も確認する。CI と同じ、ファイルを変更しない
検査には `mise run ci-build`、`mise run ci-lint`、`mise run ci-test` を使用する。

CRD、Webhook、RBAC、API 型を変更した場合は、必ず次を実行して生成物を含める。

```bash
MISE_OFFLINE=1 mise run manifests
MISE_OFFLINE=1 mise run generate
```

## テストの選び方

- Domain の状態遷移や入力検証には、外部 I/O を使わない単体テストを追加する。
- Driver や Agent protocol の契約には、Simulator または Contract Test を使う。
- Kubernetes API との統合は envtest を使う。初回は `mise run setup-envtest` を実行する。
- Kind E2E と Provisioning E2E は、手動実行する Release workflow の検証段階で実行する。通常のローカル開発では実行しない。
- OS Artifact と bootloader の変更は、Release workflow の Artifact 組み立てで該当する QEMU task を実行し、CI artifact を確認する。

テストは重要な判断または外部契約を固定するために追加する。設定ファイルの存在確認や、mock の呼出し順だけを
なぞるテストは追加しない。

## 変更の単位

1. 変更する API、Domain、Infrastructure の責務を決める。
2. 必要な生成・テストを実行する。
3. `git --no-pager diff --check` で差分を確認する。
4. 同じ目的の変更だけを 1 コミットにまとめ、`--signoff` を付ける。

利用者に見えるログ、Condition、Status message は英語で書く。credential、Bootstrap Data、署名鍵、
payload をログやテスト artifact に出力してはならない。
