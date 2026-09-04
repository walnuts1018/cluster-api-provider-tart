---
name: gotest
description: TartのGoテストを設計・実行するための保留中の規約
when_to_use: Go testの追加または実行が明示的に許可された時だけ使用する
---

# Go testの扱い

現在のTart再設計では、開発速度を優先するため新しいGo testを追加せず、Go testも実行しない。この方針が解除されるまで、このskillからテスト作成や`go test`の実行を開始してはならない。

方針解除後も、設定ファイルの存在確認やmockの呼出し順だけをなぞるテストは追加しない。外部契約、破壊的変更を防ぐ純粋な判断、controller再起動後の再計算など、失敗時の影響が大きく保守価値のある境界だけを対象にする。

テストを再開する場合の対象、実行範囲、envtestやE2Eの扱いは[検証方針](../../../docs/development/verification.md)を先に更新してから決定する。
