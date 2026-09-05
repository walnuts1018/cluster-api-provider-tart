---
name: golangci-lint
description: TartのGoコードをgolangci-lintで静的解析する
when_to_use: Goコードの変更後に静的解析を行う時
---

# 静的解析

Goコードを変更した場合は、プロジェクトで定義した`mise` taskを通じてgolangci-lintを実行する。直接バージョンの異なるbinaryを呼び出さない。lintが出したerrorは内容を理解してコードを修正し、理由のない`//nolint`で隠さない。

lint taskへ無関係なtest taskを暗黙に連鎖させない。重要な純粋判断や外部contractのGo testを別taskで実行する場合は、lintと責務を分けてCIから明示的に呼び出す。
