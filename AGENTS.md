## 前提

- チャットに対する回答、コード内のコメント、コミットメッセージは必ず日本語で行ってください。
  - ただし、アプリケーションユーザーに見えるメッセージは英語で書いてください。
    - ログやCRDの定義のコメントや、ResourceのStatusのMessageなどです。
- 変更はこまめにコミットしてください。一つのコミットは同じ内容に関する変更でまとめるようにしてください。
- コミット時には、`--signoff`オプションを使用して、コミットメッセージの末尾に署名を追加してください。ただし、Co-AuthorとしてAI Agentsを追加することはしないでください。そのコミットの内容については最終的には人間のAuthorのみが責任を負うので、Authorのみ書いてください。
- コミットメッセージは、変更内容を簡潔に説明するものにしてください。
- ブランチを切らずmainブランチへ直接コミットしてOKです。コミットも適当な粒度でどんどん行ってください。
- ツールをインストールする際は、なるべくmise経由でインストールしてください。どうしてもmiseが使えないツールを使う場合は、適切にバージョン管理&renovateで更新ができるようにしてください。
- 定期的に実行するコマンドはmiseのtaskとして定義してください。
- Kubernetesコントローラの実装、Custom ResourceやWebHookの追加には必ずcontroller-genやkubebuilderを用いてください。
- テスト用のドメインが必要な場合は、`hoge.test.walnuts.dev`や`hoge.sample.walnuts.dev`を利用して下さい。
- 開発時の検証は、可能な限りCIで再現できる形へ寄せてください。詳細は `docs/development/verification.md` を正本とし、ローカル手順だけを完了証跡にしないでください。
- 現状まだ開発中で未リリースなので、破壊的変更はどのようなものでも自由に行ってください。変更を最小にすることよりも、実装の美しさや使いやすさなどを優先してください。v1alpha1は廃止済みなので、互換性を保ったり考慮したりする必要はありません。
- コメントを書くとき、英字と日本語の間にスペース入れないでください。また、文の途中で改行しないでください。VSCodeの設定で折り返し設定を入れているので、1行が長くなるからという理由だけで改行するのは避けてください。
- sub-agentを積極的に活用してください。ただし、毎回親と同じモデルを使うのではなく、基本的にはgpt-5.6-luna (high, extra high, max)やSonnet 5 low などを活用し、なるべく低コストで済むようにしてください。重たいモデルで複数のSubAgentを使うのはなるべく避けてください。
  - When using subagents:
    - Do not repeatedly poll or wait for subagents.
    - While subagents are running, continue any useful non-overlapping work.
    - Call wait_agent only when the main task is genuinely blocked on a subagent result.
    - Prefer the longest practical wait timeout rather than frequent short polling.
    - If a wait times out and the agent is still running, do not immediately enter a repeated wait loop unless there is no other productive work available.
    - Collect and integrate completed subagent results in batches where possible.

### 開発再開

- 複数のコーディングエージェントによる開発を行う場合があります
  - .worktreeの内部で作業をしている場合は、worktreeの外部で処理を行ったりファイル書き込みを行ったりしないでください。並行で実行している作業に影響を与えてしまいます。
- 実装を行う際には、`docs/development/README.md`を確認し、アーキテクチャや検証方針を必要に応じて参照すること。

### コメント・コード規約

- 実装の意図や「なぜそう実装したか」がわかりやすいコード・命名を心がけ、不要なコメントは避けてください。
- セキュリティに関する処理（時間制限、シングルショット配信など）には意図が分かるコメントを付与してください。
- 一時的な実装には必ず `TODO:` コメントを残し、理由を明記してください。
- defer funcがエラーを返す場合は、握りつぶすのではなくlogとして出力するようにしてください。

### このファイルについて

- このファイルは、プロジェクトのコーディングスタイルや設計方針などをまとめたドキュメントです。AIツールがコードを生成・編集する際のガイドラインとして機能します。
- 詳細なアーキテクチャ設計やプロビジョニングフローについては、`architecture` スキルを参照してください。
- 開発時のアーキテクチャと検証方針は、`docs/development/README.md`を参照してください。設計方針を変更する場合は、実装と同じ変更でこのディレクトリの文書を更新してください。
- 設計や方針の変更などがあった場合には、これらのドキュメントやスキルを更新し、常に最新の状態を保つようにしてください。

## プロジェクトの概要

- 本プロジェクトは、IPMIやBMCなどの高度な管理インターフェースを持たない「一般的なデスクトップ物理PC」を対象とした、**Cluster API (CAPI) のカスタム Infrastructure Provider (cluster-api-provider-tart)** です。
- OS / Bootstrap に依存せず、Kubeadm (Ubuntu等) と Talos Linux などを共通の仕組みでプロビジョニングします。
- SSH接続によるPush型ではなく、物理PCが自ら設定を取得しにくるメタデータサーバー方式（Pull型）を採用します。
- **Monolithic Controller**: コントローラー、DHCPサーバー、TFTPサーバー、HTTPサーバーを複数のコンテナに分けるのではなく、単一のGoバイナリ（コントローラープロセス）内にすべて組み込んで実装します。これにより、K8sのステートとネットワーク応答をシームレスに同期させます。

### セキュリティとプロビジョニングのルール

- **組み込みネットワークサーバーによるProxyDHCPサポート**: 既存のネットワーク環境（ルーターやDHCPサーバー）に影響を与えないよう、IPアドレスの配布は行わず Proxy モードで iPXE のみを提供してください。Goライブラリ (`github.com/insomniacslk/dhcp`) を利用してコントローラー内で実装します。
- **Bootstrap Data の保護**:
  - メタデータ（Secret）の配信は推測不可能な One Time Token を用いて行います。
  - アクセス許可は WoL 送信からの一定時間（例: 10分）に限定し、タイムアウト処理を実装してください。
  - リプレイ攻撃を防ぐため、一度正常にダウンロードされたメタデータのトークンは即座に無効化（シングルショット配信）してください。

## 技術的な要件

### レイヤー分け

- テストがしやすいように/読む時にどこに何の処理があるかわかりやすいように、細かくレイヤーを切ってください。
- それぞれのレイヤーはInterfaceに依存するようにしてください。
- コントローラー内にドメインロジックを描かないようにしてください。

### 開発プラットフォーム・言語

- **言語**: Go言語を使用し、Kubebuilder / controller-runtime を用いた CAPI プロバイダー標準の実装方針に従ってください。
- **プラットフォーム**: オンプレミスの Kubernetes マネジメントクラスタ。ローカル開発では `kind` を使用します。

### アーキテクチャ

- CRDとして以下の2つを定義・管理します。
  - `TartHost`: 物理PCのインベントリ管理（MACアドレスとステータスを保持）。
  - `TartMachine`: CAPI の `Machine` に対応するインフラリソース（ブートOSイメージやカーネルパラメータなどを保持）。
- **Infrastructure Controller (The Brain)**: 単一のPod (`hostNetwork: true`) で稼働し、以下の機能をGoroutineとして並行起動します。
  - **K8s Reconciler**: CRDの監視、PCの割り当て、WoL送信、トークン管理。
  - **Embedded DHCP Server**: `insomniacslk/dhcp` を利用。PXEブート要求を捕捉し、iPXEブートローダのパスを応答。
  - **Embedded TFTP Server**: `pin/tftp` を利用。アーキテクチャに応じた iPXE バイナリ (`ipxe-x86_64.efi`, `ipxe-arm64.efi`) を配信。
  - **Embedded HTTP Server**: カーネル/initrd、動的iPXEスクリプト、および機密データ (Bootstrap Secret) をセキュアに配信。

## リトライ

- このアプリケーションでは、ネットワーク越しの通信や、別ホストで実行する処理などが存在し、それらの処理の失敗やタイムアウトについて考慮する必要があります。
  - リトライを行う場合は、`github.com/avast/retry-go/v4`などを利用して、指数バックオフやリトライ回数・時間の制限を設けるようにしてください。

## OpenTelemetry

- TracerやMeterは、`telemetry.Tracer`などのグローバル変数から取得してください。TracerProviderやMeterProviderも、`otel.GetTracerProvider()`などを用いてグローバルに取得して下さい。
- TracerやMeterの設定について、サンプリングレートやExport先のアドレスなどは、`OTEL_TRACES_SAMPLER`といった環境変数から動的に設定する機能が`go.opentelemetry.io/otel/`側に備わっています。私たちのコード側で勝手に固定値に設定したり、独自の環境変数パースロジックを実装したりすることは禁止です。

## Cluster API

- 詳細なCluster APIの実装ルールについては、`cluster-api` スキルを参照してください。
