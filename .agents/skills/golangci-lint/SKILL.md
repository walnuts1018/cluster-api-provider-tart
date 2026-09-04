---
name: golangci-lint
description: TartのGoコードをgolangci-lintで静的解析する
when_to_use: Goコードの変更後に静的解析を行う時
---

# 静的解析

Goコードを変更した場合は、プロジェクトで定義した`mise` taskを通じてgolangci-lintを実行する。直接バージョンの異なるbinaryを呼び出さない。lintが出したerrorは内容を理解してコードを修正し、理由のない`//nolint`で隠さない。

現在の再設計でGo testは追加・実行しないため、lint taskへtest taskを連鎖させない。lintの対象は新しいルート直下の責務別packageだけとし、削除した旧`domain`、`infrastructure`、agent、artifactを前提にしない。
