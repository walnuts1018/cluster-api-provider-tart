# 改善・修正台帳

この文書は、リポジトリ全体を対象にした初回監査で見つかった不具合、誤解を招く構成、保守性の課題、CIで不足している検証を列挙する。再設計ドキュメントの全面改訂前でも、実装の着手順と未完了範囲を追跡できる最低限の正本として使用する。

## 監査範囲と判定

- 対象: Goコード、CRD/Deployment/Kustomize、artifact生成処理、mise task、GitHub Actions、テスト、既存ドキュメント。
- `env GOCACHE=$(pwd)/.cache/go-build go test ./...` を実行した。通常のパッケージは通過したが、sandbox の `listen ...: bind: operation not permitted` により Redfish simulator 契約テストと WoL UDP テストが失敗した。CIではネットワーク権限を持つジョブで再実行し、環境失敗と回帰を別の結果として保存する。
- 作業開始時点で既存の未コミット変更（`docs/redesign/ci-verification.md`、`target-state.md`、Task 08/09、`writer_test.go`）がある。これらは本監査の変更と混ぜず、意図を確認してから統合する。

## 優先度 P0: 先に直すべき安全性・再現性

| 状態 | 対象 | 課題 | 改善案 | 完了条件 |
|---|---|---|---|---|
| 完了 | `config/prometheus/monitor.yaml` | `insecureSkipVerify: true` が標準マニフェストで有効だった。 | cert-manager管理SecretのCA参照へ変更し、TLS検証を既定で有効化した。 | render後のServiceMonitorが検証を無効化しない。 |
| 一部完了 | `.github/workflows/*`、`mise.toml` | OS/Kubernetes更新時に実workload clusterのResource UID、PVC UID、payload digestを比較するCIジョブがない。 | UID/digest比較器の不具合を修正し、入力検証と改変検出のCI契約を追加した。次に実workload clusterからの採取とOSOnly/KubernetesBinary更新へ接続する。 | OSOnly更新とKubernetesBinary更新の両方で、Resource/PVCデータ保持の証跡が1 runに残る。 |
| 一部完了 | `.github/workflows/ci.yaml`、`.github/filters.yml` | `go`/`lint` フィルターが重要なビルド・workflow変更を網羅していなかった。 | Dockerfile、Makefile、hack、artifact、workflow、renovateをgo/lint対象へ追加した。フィルター契約テストは残課題。 | 各主要ディレクトリの変更に対するフィルター契約テストまたはactionlint検査が通る。 |
| 完了 | `mise.toml` の `docker-buildx` | build/pushの失敗を `|| true` で握りつぶしていた。 | build/createの失敗を伝播させ、`trap`で一時ファイルとbuilderを後始末するよう変更した。 | push失敗時にtaskとCIが失敗し、後始末だけは実行される。 |
| 未着手 | `artifact/mkosi`、`config/*`、`test/e2e/*` | `latest` タグ、外部URLのmutable参照が複数存在し、同じコミットを再現できない。 | digestまたは固定バージョンを既定値にし、更新は依存更新PRで行う。 | CI再実行で同じ入力digestが得られ、mutable参照のallowlist違反がない。 |

## 優先度 P1: 機能欠落・回帰リスク

| 状態 | 対象 | 課題 | 改善案 |
|---|---|---|---|
| 一部完了 | `.github/workflows/e2e-provisioning.yaml` | Provisioning E2Eは失敗時のログ中心だった。 | 成功時も実行証跡をuploadするよう変更した。Resource/PVC保持比較は残課題。 |
| 未着手 | `.github/workflows/os-artifact.yaml` | 失敗時のみ証跡をuploadするため、成功runから受入証跡を追跡しにくい。 | `if: always()` のsummary uploadを追加し、manifest/provenance/digestを保持する。 |
| 完了 | `pkg/telemetry/otel.go` | `ServiceVersion: "latest"` は観測データをリリースへ関連付けられなかった。 | build情報のversionを利用し、開発ビルドは `dev` とする。 |
| 未着手 | `config/manager/manager.yaml`、`config/bootstrap/*` | `TODO(user)` や生成元のプレースホルダーが残り、resources・volume・hostNetworkの意図がマニフェストだけでは判別しにくい。 | コメントを客観的なWhyへ置換し、resource値・hostNetwork・bootloader供給の契約を検証する。 |
| 完了 | `test/e2e/e2e_suite_test.go`、`cmd/main.go` | Kubebuilder由来の `TODO(user)` コメントが残っている。チーム向けの制約・置換条件が書かれていない。 | Kubebuilder由来のコメントを、現在の起動・E2E目的を表す客観的なコメントへ置換した。 |
| 未着手 | `pkg/gomega/have_fields_test.go` | 内容のない `TODO` がテストに残っている。 | 意図がなければ削除し、必要ならテストが固定するWhatを明記する。 |
| 未着手 | `test/e2e/e2e_test.go` | `example.com` と `:latest` のイメージを使用し、実運用の認証・固定性を検証できない。 | CI用の固定digestイメージとテスト専用ドメインへ置換する。 |

## 優先度 P2: 構造・保守性

| 状態 | 対象 | 課題 | 改善案 |
|---|---|---|---|
| 未着手 | `internal/application/**` | workflowごとに `model`、`port`、`step`、`handler` などの分割粒度が不均一で、空に近いパッケージもある。 | DMMFの境界（domain/application/adapter）を維持しつつ、公開されない1ファイルパッケージを親packageへ統合する。依存方向を静的検査する。 |
| 未着手 | `cmd/`、`internal/controller/` | composition rootとcontrollerの責務が肥大化しやすい。 | wireで組み立てる依存を明示し、controllerはworkflow起動とKubernetes I/Oだけに限定する。 |
| 未着手 | `hack/` | artifact、QEMU、Redfish、manifest処理が横並びで、CIからの呼出し契約が暗黙的。 | `hack/<capability>` ごとに入力・出力artifact schemaを固定し、共通のevidence writerを抽出する。 |
| 未着手 | `Makefile` と `mise.toml` | 同じ処理の定義が二重化し、失敗時の挙動や環境変数が一致しない可能性がある。 | miseを正本にしてMakefileは薄い互換ラッパーへ縮小し、task名と引数を検査する。 |
| 未着手 | 生成物（CRD、DeepCopy、wire） | 生成元変更と生成物差分の検証がジョブごとに分散している。 | `generate`、`manifests`、wireの再生成を1つのCI契約にまとめ、差分検査を常時実行する。 |

## 優先度 P3: テスト品質・開発体験

- Redfish/WoLのソケット依存テストを、CIの権限付き統合テストと、ソケットを使わない純粋なドメイン/driver contract testへ分離する。
- `go test ./...` の失敗を「sandbox環境」「外部依存」「コード回帰」に分類して報告するスクリプトを追加する。
- CIで実行されるテスト名とTask受入条件の対応表を作り、存在確認だけのテストは追加しない。
- 依存更新、生成差分、Kustomize render、actionlint、shellcheckを同じ検証入口から実行できるようにする。
- ログ、Condition、artifact summaryからcredential・Bootstrap Data・PVC payloadが漏えいしないことを検査する。

## 実装順序

1. CIフィルター、失敗握りつぶし、mutable tag、TLS既定値を修正する。
2. Cluster lifecycle E2E（OSOnly/KubernetesBinary更新とResource/PVC保持）を実装する。
3. OS Artifact/Provisioning E2Eの成功証跡を保存する。
4. telemetry version、TODOコメント、マニフェストのプレースホルダーを整理する。
5. DMMF境界を壊さない範囲でapplication/hack/package構造を統合し、生成・lint・テスト契約を強化する。

各段階は機能単位で日本語のsignoff付きコミットに分け、CIで再現できる完了証跡を残す。ドキュメント全面改訂時は、この台帳を新しいタスク/ADRへ移管し、完了項目を削除せず履歴として参照可能にする。
