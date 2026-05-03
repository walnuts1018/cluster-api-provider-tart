# Mise Task Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Makefile に残っている実処理を mise task へ移し、kubebuilder 互換の Make target は mise task を起動する薄い入口にする。

**Architecture:** `mise.toml` をタスク実行の主にし、生成、テスト、lint、build、Docker、deploy、依存ツール確認を同名 task として定義する。`Makefile` は同名 target を残し、`IMG` や `CONTAINER_TOOL` など kubebuilder の利用者が渡す変数を環境変数として `mise run` へ転送する。

**Tech Stack:** mise task、Make、Go、kubebuilder/controller-tools、controller-runtime envtest、golangci-lint。

---

### Task 1: mise task を実処理の主にする

**Files:**
- Modify: `mise.toml`

- [ ] **Step 1: 既存 Makefile の処理を mise task として定義する**

`manifests`、`generate`、`fmt`、`vet`、`test`、`lint`、`build`、`run`、Docker、deploy、envtest 関連を `mise.toml` に移す。

- [ ] **Step 2: `mise run test` と `mise run lint` を検証する**

Run: `mise run test` and `mise run lint`
Expected: both pass.

### Task 2: Makefile を kubebuilder 互換ラッパーにする

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Make target を残して mise task を起動する**

kubebuilder が呼び出す `make manifests`、`make generate`、`make install` などの target 名を維持し、処理は `mise run <task>` へ委譲する。

- [ ] **Step 2: Make 経由の代表 target を検証する**

Run: `make generate`, `make manifests`, `make lint`
Expected: all call mise task and pass.

### Task 3: 最終検証

**Files:**
- Verify only.

- [ ] **Step 1: Go tests を実行する**

Run: `mise run test`
Expected: PASS.

- [ ] **Step 2: lint を実行する**

Run: `mise run lint`
Expected: PASS with `0 issues`.

- [ ] **Step 3: Make wrapper を確認する**

Run: `make help`, `make lint`
Expected: both execute through mise.
