---
name: golangci-lint
description: golangci-lintを用いた静的解析とコード修正
when_to_use: Goのコードを修正・追加した後、またはコード規約のチェックを行う時
---

## golangci-lint の実行

Goコードを変更した後は、必ず `golangci-lint run` を実行して静的解析を行ってください。
プロジェクトルートで以下のコマンドを実行します。

```bash
golangci-lint run
```

## エラーの修正方針

- エラーが出た場合は、指摘内容を理解した上でコードを修正してください。
- 警告を無視するための `//nolint:...` コメントは、正当な理由がない限り使用しないでください。使用する場合は、必ず理由を併記してください。
- フォーマットやimport順の自動修正には `golangci-lint run --fix` も活用できますが、何が修正されたか `git diff` で確認するようにしてください。
