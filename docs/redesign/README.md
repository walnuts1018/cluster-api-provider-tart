# ベアメタル基盤プロバイダー再設計

## 目的

このディレクトリは、多様な物理ホストに対してOSの初期導入とA/B更新を行うCluster API Infrastructure Providerへの再設計方針を定義する。現在の実装をそのまま拡張するための仕様ではなく、移行先の境界、段階、完了条件を定める計画文書である。

## 文書一覧

- [記述規約と用語集](conventions.md)
- [達成すべき状態](target-state.md)
- [アーキテクチャ](architecture.md)
- [全体の実装計画](implementation-plan.md)
- [ADR一覧](adr/README.md)
- [タスク一覧](tasks/README.md)

## 設計上の重要な前提

1. `TartMachine`はCAPIのInfraMachine契約を守り、`TartHost`はMachineとは独立した物理インベントリとして存続する。
2. 初期プロビジョニングとインプレース更新を分ける。後者はCAPI Runtime SDKのIn-Place Update Hooksから開始する。
3. Infrastructure ProviderはBootstrap Providerを置き換えない。Bootstrap Dataを安全に対象ホストへ運び、OS上で一度だけ実行可能な形に配置する。
4. OS成果物はwhole-disk imageではなく、固定サイズのOSスロットへ書けるファイルシステムイメージとマニフェストを基本単位にする。
5. OSスロットは単なる`ro` mountではなくdm-verityで検証し、Boot/OS/Verity/State/Dataという論理roleをプラットフォームごとの物理レイアウトへ写像する。
6. Ubuntu/DebianのA/B構成とRaspberry Pi固有のブート構成は、同一レイアウトを強制せず、明示的なプラットフォームプロファイルで分ける。
7. 外部プラグインABIを先に固定しない。まずGoのCapability別インターフェースでWoLとRedfishを実装し、その意味論を検証した後にversioned gRPC APIを追加する。

## 読み方

1. 最初に[記述規約と用語集](conventions.md)を読む。
2. [達成すべき状態](target-state.md)で完成条件と対応範囲を確認する。
3. [アーキテクチャ](architecture.md)でコンポーネント、CR、状態遷移、処理順を確認する。
4. 判断理由が必要な場合は[ADR一覧](adr/README.md)を参照する。
5. 実装時は[タスク一覧](tasks/README.md)から依存関係と受け入れ条件を確認する。

文書中の「必須」「禁止」「推奨」「任意」「未決定」は、[要求レベル](conventions.md#2-要求レベル)の意味で使用する。英語の用語を独自解釈せず、用語集の定義を使用する。

## 文書の位置付け

- `Accepted`のADRは実装時の既定方針である。
- `Proposed`のADRは、先行タスクの検証が完了するまで確定仕様として扱わない。
- タスクの受け入れ条件を満たさずに次の依存タスクへ進まない。
- 実装中に前提が崩れた場合は、コードより先にADRと関連文書を更新する。
