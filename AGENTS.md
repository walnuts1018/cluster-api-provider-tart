# Tart Agent Rules

このファイルは、Tartの全ての作業へ常時適用する短い規約である。設計や実装の詳細は、必要な作業に対応するskillと`docs/development/`を参照する。

## 常時適用する規約

- チャット、コードコメント、コミットメッセージは日本語で書く。ただし、log、Event、Condition、Status messageなどアプリケーション利用者に見えるメッセージは英語で書く。
- コメントは作業者との会話ではなく、コードの意図や安全上の理由を説明する。
- コメントを書くとき、英字と日本語の間にスペース入れないでください。また、文の途中で改行しないでください。VSCodeの設定で折り返し設定を入れているので、1行が長くなるからという理由だけで改行するのは避けてください。すでにあるコメントについても、気づいた時に積極的に修正してください。
- 一時的な実装には、理由と解消条件を含む`TODO:`を残す。`defer`した処理のerrorを握りつぶさず、必要ならlogへ出力する。
- 変更は同じ目的ごとにまとめ、コミット時は`--signoff`を使う。AI AgentをCo-Authorへ追加しない。mainへ直接コミットしてよい。
- ツールは可能な限りmiseで管理し、定期的なコマンドはmise taskとして定義する。
- Kubernetes controller、CRD、Webhookの変更にはkubebuilderやcontroller-genを使う。
- `docs/development/README.md`を開発の入口とし、要件・非目標を変えた場合は同じ変更で更新する。
- 何かしらの外部ツールやパッケージなどのバージョンをファイルとして記述する場合は必ず適切にrenovateで管理・アップデートできるようにしてください。複数の場所でバージョンを整合させる必要がある場合も、それをうまくrenovateで扱えるようにしてください。
- 並列に複数のCoding Agentが作業することがあります。他のエージェントの作業を上書きしないように、他のエージェントが作業している箇所を編集しないようにしてください。
- ダミーやテスト用のドメインには`test.walnuts.dev`や`sample.walnuts.dev`を使う。また、テストなどでのMACアドレスは00-00-5E-00-53-00から00-00-5E-00-53-FFの範囲を使う。IPアドレスはRFC 5737で予約された範囲を使う。
- sub-agentを積極的に活用してください。ただし、毎回親と同じモデルを使うのではなく、基本的にはgpt-5.6-luna (medium, high, xhigh)やSonnet 5 (low) などを活用し、なるべく低コストで済むようにしてください。重たいモデルで複数のSubAgentを使うのはなるべく避けてください。
  - When using subagents:
    - Do not repeatedly poll or wait for subagents.
    - While subagents are running, continue any useful non-overlapping work.
    - Call wait_agent only when the main task is genuinely blocked on a subagent result.
    - Prefer the longest practical wait timeout rather than frequent short polling.
    - If a wait times out and the agent is still running, do not immediately enter a repeated wait loop unless there is no other productive work available.
    - Collect and integrate completed subagent results in batches where possible.

## ディレクトリ構成

- ルート直下に`internal`または`pkg`という名前のディレクトリを作成しない。
- interfaceは、外部副作用を隔離する、または実際に複数の実装が存在する場合だけ定義する。将来の可能性だけで抽象化しない。

## Tartの範囲

- TartはTalos Linux専用のInfrastructure Provider、Bootstrap Provider、Control Plane Providerである。Kubeadm、Ubuntu、汎用OS provisioning framework、既存Talos Providerへ対応するための互換層は作らない。
- Talosのinstaller、machine configuration、disk/volume、upgrade、rollback、etcd bootstrap、Kubernetes runtimeを再実装しない。
- `TartHost`はCAPI Machineより長寿命のinventory、`TartMachine`はCAPI Machineとのbindingを表す。通常のupdateでHost claim、Machine、diskを破棄しない。
- `TartHostOperation`、Operation CRD、Workflow engine、Provisioning Plan、独自Provisioning Agent、独自Node Lifecycle Agent、独自OS image format、独自disk writer、add-on専用APIを作らない。
- Provider APIは`v1alpha1`へリセットし、Infrastructure、Bootstrap、Control Planeをそれぞれ`infrastructure.cluster.x-k8s.io`、`bootstrap.cluster.x-k8s.io`、`controlplane.cluster.x-k8s.io`へ分ける。CAPI coreの現行v1beta2 contractとは別のversionである。

## 安全性

- Resource Statusは観測結果とConditionsだけを保持し、workflowのprogram counterやstep番号として利用しない。controller再起動後も外部状態の観測からreconcileを継続できるようにする。
- in-place updateできない変更をMachine replacementへ暗黙にfallbackせず、`Ready=False`で安全停止する。unsafe/unknown/partial diffは同様にfail-closedで止める。
- Machine削除後もHostのデータは保持し、明示的な承認なしに自動allocationしない。停止未確認時はclaimを解除しない。
- ユーザーのraw Talos configuration patchは全てimmutableなSecret-backed inputへ格納し、CRD Specへinline保存する経路を作らない。
- Secret、credential、private key、Bootstrap Data、kubeconfigをStatus、Event、通常log、metrics labelへ出力しない。

## 作業開始時の参照先

- 達成すべき要件・非目標: `docs/development/README.md`
- 設計・実装レビューの着眼点: `.agents/skills/`配下の各SKILL.md(architecture、cluster-api、reconcile、talos、host-lifecycle、observability、gotest、golangci-lint、general-coding-style)

## 現在の検証方針

Go testを全面禁止しない。失敗時の影響が大きい境界(Host claim race、fail-closed判定、quorum判定、外部contractなど)へ最小限のtable test、fuzz test、契約テストを追加する。設定ファイルの存在確認やmock呼出し順だけのテストは追加しない。静的検証(format、generate、manifests、lint、build、vet)と、実機依存のE2Eは責務を分けて実行する。
