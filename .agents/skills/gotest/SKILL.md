---
name: gotest
description: TartのGoテストを設計・実行するための規約
when_to_use: 破壊的な判断、外部contract、controller再起動後の再計算を検証するGo testを設計・実行する時
---

## 大方針

不要なテストは絶対に追加しないでください。
例えば設定ファイルが存在することを確かめるテストや、ほとんどがモックを順番に呼ぶだけのユニットテストなど、存在する意味がほとんどないテストはおかないてください。

テストというのは多ければ多いほどいいわけではありません。テストの品質を保ち、安定して動作させるのも大変なことです。とにかくテストケースをたくさん増やしてカバレッジだけ高いテストというのは必ずしもいいテストとは言えません。クリティカルなパスや純粋関数のロジック部分など、必要な部分をちゃんとテストしつつ、必要ない部分のテストは省いてしまいましょう。

また、無理に新しい実装をするときにテストから用意する必要もありません。実装してみないとわからないこともたくさんあります。
初期の段階ではテストを省きながら進めてスピードを出すことも重要です。

## テストの実行

Goのテストを実行する際は、以下のコマンドを実行して全体がパスすることを確認してください。

```bash
go test ./... -v
```

## テスト実装のガイドライン

- **Table Driven Tests**: Goの慣習に従い、入力と期待される出力を構造体のスライスとして定義する Table Driven Tests を積極的に活用してください。
- **BDD（振る舞い駆動開発）テスト**: 必要に応じてBDDスタイルのテストも検討してください。その場合は、ginkgo/v2とgomegaを利用してください。
- **カバレッジ**: 重要なロジック（コントローラーのReconcile、ProxyDHCPのロジック、メタデータ配信のセキュリティ検証など）に対しては、正常系だけでなく異常系やタイムアウトなどのエッジケースもしっかりとカバーしてください。
- **モック**: 外部依存がある場合は、必要に応じて `gomock` やインターフェースを活用してモックを作成し、単体テストが独立して実行できるようにしてください。

## 実装中のテスト

実装を早く完成させることを最優先に取り組む。バグや実装の綺麗さ・レイヤー分けの正確さなどは実装が完了してから修正していけば良い。テストについても基本的には最低限のテストに留め、実装が完了してから網羅的にテストを追加する。失敗時の影響が大きく副作用から分離できる純粋な判断へtable testや必要最小限のfuzz testを追加する。

対象はHost claimの競合結果、Retained gate、Cluster ID不一致によるbundle/Adopt拒否、TemplateやSSA dry-runでIDを生成しないこと、preset IDの通常CREATE拒否とrestore-approved復元、pending secret generationをrotation開始前に永続化すること、rotation対象外CAやkeyを変更しないこと、unsafe diffのfail-closed判定、Secret参照名ではなくresolved configurationのsemantic diffを使うこと、availability理由と`allowDowntime`の判定、reuse approvalの世代不一致、quorum判定、configuration invariant conflict、redacted semantic digestなどとする。

Kubernetes API、CAPI Runtime Extension、Webhook、Secret contract、controller restartは必要に応じてenvtestまたは契約テストで検証する。CAPI minorごとのunsafe diffでMachineSet、Machine、TartHost claimが作られないこと、実機のTalos、storage、reboot、rollback、drainはE2Eで検証する(実機・VMでの検証はユーザー自身が行う)。
