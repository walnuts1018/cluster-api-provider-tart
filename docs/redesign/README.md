# ベアメタル基盤プロバイダー再設計

## 目的

このディレクトリは、多様な物理ホストに対してOSの初期導入とA/B更新を行うCluster API Infrastructure Providerへの再設計方針を定義する。現在の実装をそのまま拡張するための仕様ではなく、移行先の境界、段階、完了条件を定める計画文書である。

## 文書一覧

- [記述規約と用語集](conventions.md)
- [達成すべき状態](target-state.md)
- [アーキテクチャ](architecture.md)
- [全体の実装計画](implementation-plan.md)
- [Platform Profile一覧](platform-profiles/README.md)
- [Runbook一覧](runbooks/README.md)
- [v1alpha1からv1beta1への移行](migration-v1alpha1-to-v1beta1.md)
- [ADR一覧](adr/README.md)
- [タスク一覧](tasks/README.md)

## 設計上の重要な前提

1. `TartMachine`はCAPIのInfraMachine契約を守り、`TartHost`はMachineとは独立した物理インベントリとして存続する。
2. 初期プロビジョニングとインプレース更新を分ける。後者はCAPI Runtime SDKのIn-Place Update Hooksから開始する。
3. Infrastructure ProviderはBootstrap Providerを置き換えない。Bootstrap DataはHost/Operationへ結び付けた10分TTLのSession Tokenで1回だけ配信し、OS上でpayload digestごとに1回だけ適用する。
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

## 実装中の進め方

再設計の実装中は、実機または管理クラスタで一連の処理を早期に動かし、設計上の仮定を検証することを優先する。網羅的なテスト、抽象化の完成、細部の整形を理由に、次の動作確認可能な縦方向の実装を長期間止めない。

ただし、試行錯誤を可能にするため、次の事項は先送りしない。

- 状態遷移、Host選択、互換性判定などの純粋な判断はI/Oから分離し、代表的な正常系と失敗系を単体テストする。
- 長時間処理の状態はKubernetes Resourceへ保存し、process再起動後に失われるメモリ内状態を正本にしない。
- 失敗箇所をResourceのCondition、Event、構造化logのいずれかから特定できるようにする。credentialやBootstrap Dataは出力しない。
- 一時的な実装には、残る制約と置き換える条件を記した`TODO:`を付ける。
- CRD、Webhook、Controllerの生成と更新にはKubebuilderまたはcontroller-genを使用する。

タスクの受け入れ条件は最終的な完了判定として維持する。実装途中のコミットでは、未検証の条件と実機確認が必要な事項をタスク文書へ記録してよい。
