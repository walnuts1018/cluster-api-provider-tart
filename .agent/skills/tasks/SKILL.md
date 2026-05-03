---
name: tasks
description: GitHub Issueを利用したタスク管理と進行方法
when_to_use: 開発の開始時、次にどのタスクに着手するか判断する時、タスクの進捗を確認する時
---

# タスク管理ガイドライン

本プロジェクトでは、タスクの一覧や進捗状況はファイル（TASKS.mdなど）ではなく、**すべてGitHub Issues** で一元管理します。

## AIセッション時のワークフロー

AIエージェントによる開発セッションを開始、または次のタスクに移行する際は、以下の手順でIssueを利用して進行してください。

### 1. タスクの確認
常に `gh` コマンドを用いて現在のタスク一覧を確認してください。

```bash
# OpenなIssue一覧を取得する
gh issue list
```

### 2. タスクの選択と詳細確認
着手するタスク（Issue）を決定し、その詳細な要件を確認してください。

```bash
# 例: Issue番号が6の場合
gh issue view 6
```

### 3. ブランチの作成と開発
選択したIssueに対応する機能ブランチを作成して実装を行います。

```bash
# 例: Issue 6 に対応するブランチ
git checkout -b feature/issue-6-proxy-dhcp
```

### 4. コミットとPull Request
実装が完了したらコミット（`--signoff` 必須）し、Pull Requestを作成します。
PRの本文には、`Closes #<Issue番号>` や `Resolves #<Issue番号>` を含めて、マージ時にIssueが自動的にクローズされるようにしてください。

```bash
# Pull Requestの作成例
gh pr create --title "feat: Implement ProxyDHCP for Bootstrapper" --body "Resolves #6

## 変更内容
- dnsmasqの設定を追加
- ProxyDHCPモードの有効化"
```

この手順に従うことで、人間とAIエージェント間で常に一貫したタスク状態の共有が可能になります。