---
name: general-coding-style
description: TartのGoコード、コメント、エラー処理の基本規約
when_to_use: TartのGoコードを作成・変更・レビューする時
---

# Go実装規約

## 言語とメッセージ

- チャット、コードコメント、コミットメッセージは日本語で書く。
- controllerのlog、Event、Condition message、Status messageなど利用者に見えるアプリケーションメッセージは英語で書く。
- コメントは実装の意図や安全上の理由だけを書く。指示者との会話や作業状況を前提にしたコメントを書かない。
- 一時的な実装には`TODO:`と理由、解消条件を残す。
- 日本語の文章中で英単語の前後に不要な半角スペースを入れず、文の途中で改行しない。

## パッケージ

- ルート直下に`internal`または`pkg`を作らない。
- packageは`api`、`controller`、`host`、`talos`、`bootstrap`、`controlplane`、`boot`、`extensions`のように、現在の責務を直接表す名前へ置く。
- interfaceは、外部副作用を隔離する、または複数の具体的実装が実際に存在する場合だけ定義する。将来の可能性だけで抽象化しない。
- TalosやCAPIのgenerated typeを広い層へ漏らさず、adapterがTartで必要な意味へ変換する。

## Goの安全性

- `defer`した関数がerrorを返す場合は、握りつぶさずstructured logへ出力する。
- URLは文字列結合で組み立てず、`net/url`など適切なAPIを使う。
- `errors.Is`、`errors.As`、sentinelまたは型付きerrorを使い、文字列比較でerror分類を行わない。
- 外部入力をそのままlog、Event、Condition、metrics labelへ出さない。特にSecret、credential、private key、Bootstrap Dataを出力しない。
- contextのdeadlineとcancelを外部APIへ伝播し、reconcile中に無制限のgoroutineやretryを作らない。
- resourceのidentityをnameの慣習やprocess memoryだけで保持しない。UID、reference、stable hardware identity、外部APIの観測結果を使う。

## Talos固有の禁止事項

Talosのinstaller、machine configuration、disk/volume、upgrade、rollback、etcd bootstrapをTart独自機能として再実装しない。Tartの判断はTalos APIとTalos-native configurationへ委譲し、意味を解釈できない差分は安全側へ倒して`Ready=False`とreasonを設定する。
