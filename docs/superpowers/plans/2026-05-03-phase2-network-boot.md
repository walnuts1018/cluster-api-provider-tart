# Phase 2 Embedded Network Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `dnsmasq` を使った外部コンポーネント構成を廃止し、コントローラー自身に DHCP/TFTP サーバー機能を組み込むことで、Phase 2 の最小ネットワークブート基盤を Go ネイティブに実装する。

**Architecture:** コントローラープロセス内で `insomniacslk/dhcp` (DHCP) と `pin/tftp` (TFTP) ライブラリを使用し、Goroutine としてサーバーを起動する。マネージャープロセスには HTTP サーバーも同居させ、iPXE スクリプトを配信する。

**Tech Stack:** Go、`github.com/insomniacslk/dhcp`、`github.com/pin/tftp`、controller-runtime manager。

---

### Task 1: 組み込み DHCP サーバーの実装

**Files:**
- Create: `internal/server/bootstrapper/dhcp.go`
- Create: `internal/server/bootstrapper/dhcp_test.go`

- [ ] **Step 1: Write the failing test**
- [ ] **Step 2: Implement ProxyDHCP server using `insomniacslk/dhcp`**
- [ ] **Step 3: Verify DHCP response matches expectation**

### Task 2: 組み込み TFTP サーバーの実装

**Files:**
- Create: `internal/server/bootstrapper/tftp.go`
- Create: `internal/server/bootstrapper/tftp_test.go`

- [ ] **Step 1: Write the failing test**
- [ ] **Step 2: Implement TFTP server using `pin/tftp`**
- [ ] **Step 3: Verify file delivery (ipxe.efi)**

### Task 3: manager への統合とライフサイクル管理

**Files:**
- Modify: `cmd/main.go`
- Modify: `internal/server/bootstrapper/bootstrapper.go`

- [ ] **Step 1: DHCP/TFTP サーバーを `manager.Runnable` としてラップ**
- [ ] **Step 2: `cmd/main.go` でのサーバー起動フラグと初期化処理の追加**
- [ ] **Step 3: hostNetwork 有効化に向けたマニフェスト調整 (config/manager)**

### Task 4: ダミー iPXE スクリプトと全体検証

**Files:**
- Modify: `internal/server/ipxe/server.go`

- [ ] **Step 1: DHCP/TFTP と連携する動的な iPXE スクリプト生成の基礎実装**
- [ ] **Step 2: ローカル環境でのネットワークブート・シーケンスのモック検証**
- [ ] **Step 3: ドキュメント更新**
