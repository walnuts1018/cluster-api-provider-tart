# Phase 2 Network Boot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** dnsmasq を使った ProxyDHCP/TFTP 用 Kubernetes マニフェストと、ダミーの iPXE スクリプトを返す HTTP サーバーを追加して Phase 2 の最小ネットワークブート基盤を用意する。

**Architecture:** マネージャープロセスに小さな HTTP サーバーを同居させ、`/ipxe` で固定内容のスクリプトを返す。dnsmasq は controller から独立したマニフェスト群として `config/bootstrap` に置き、将来の動的生成や metadata server 追加時にも責務を分けたまま拡張できるようにする。

**Tech Stack:** Go、標準ライブラリ `net/http`、controller-runtime manager、Kubernetes YAML、kustomize。

---

### Task 1: ダミー iPXE スクリプトサーバー

**Files:**
- Create: `internal/server/ipxe/server.go`
- Create: `internal/server/ipxe/server_test.go`

- [ ] **Step 1: Write the failing test**

```go
package ipxe_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/internal/server/ipxe"
)

func TestHandlerServesDummyScript(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ipxe", nil)
	rec := httptest.NewRecorder()

	ipxe.NewHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/plain") {
		t.Fatalf("content-type = %q, want text/plain", contentType)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "#!ipxe") {
		t.Fatalf("body = %q, want iPXE header", body)
	}
	if !strings.Contains(body, "echo Tart placeholder boot script") {
		t.Fatalf("body = %q, want placeholder message", body)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ipxe -run TestHandlerServesDummyScript -v`
Expected: FAIL because the package and handler do not exist yet.

- [ ] **Step 3: Write minimal implementation**

```go
package ipxe

import (
	"fmt"
	"net/http"
)

const dummyScript = `#!ipxe
echo Tart placeholder boot script
sleep 3
`

func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ipxe", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if _, err := fmt.Fprint(w, dummyScript); err != nil {
			http.Error(w, "failed to write response", http.StatusInternalServerError)
		}
	})
	return mux
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ipxe -run TestHandlerServesDummyScript -v`
Expected: PASS.

- [ ] **Step 5: Commit**

Run: `git --no-pager add internal/server/ipxe/server.go internal/server/ipxe/server_test.go && git --no-pager commit --signoff -m "Add dummy iPXE script server"`

### Task 2: manager への HTTP サーバー接続

**Files:**
- Modify: `cmd/main.go`
- Test: `internal/server/ipxe/server_test.go`

- [ ] **Step 1: Write the failing test**

`server_test.go` に `/healthz` ではなく `/ipxe` だけを公開する構成でも handler が standalone で動くことを確認するサブテストを追加し、manager 側から利用される API を固定する。

```go
func TestHandlerRejectsNonGET(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/ipxe", nil)
	rec := httptest.NewRecorder()

	ipxe.NewHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ipxe -run TestHandlerRejectsNonGET -v`
Expected: FAIL because the handler does not reject POST yet.

- [ ] **Step 3: Write minimal implementation**

`cmd/main.go` に `--ipxe-bind-address` flag を追加し、`0` 以外なら別 goroutine で `http.Server` を起動する。handler は `ipxe.NewHandler()` を使い、manager 停止時に `Shutdown` されるよう `mgr.Add` でライフサイクル管理する。

```go
var ipxeBindAddress string
flag.StringVar(&ipxeBindAddress, "ipxe-bind-address", ":8082", "The address the iPXE script endpoint binds to. Use 0 to disable.")
```

```go
if ipxeBindAddress != "0" {
	if err := mgr.Add(ipxe.NewRunnable(ipxeBindAddress, ipxe.NewHandler(), setupLog.WithName("ipxe"))); err != nil {
		setupLog.Error(err, "Failed to add iPXE server")
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ipxe -v`
Expected: PASS.

- [ ] **Step 5: Commit**

Run: `git --no-pager add cmd/main.go internal/server/ipxe/server.go internal/server/ipxe/server_test.go && git --no-pager commit --signoff -m "Wire iPXE server into manager"`

### Task 3: dnsmasq ProxyDHCP/TFTP マニフェスト

**Files:**
- Create: `config/bootstrap/kustomization.yaml`
- Create: `config/bootstrap/namespace.yaml`
- Create: `config/bootstrap/bootstrap_configmap.yaml`
- Create: `config/bootstrap/bootstrap_deployment.yaml`
- Create: `config/bootstrap/bootstrap_service.yaml`

- [ ] **Step 1: Write the failing test**

最小の構成検証として、kustomize build が成立することを失敗条件にする。

Run: `mise run kustomize build config/bootstrap`
Expected: FAIL because the directory and manifests do not exist yet.

- [ ] **Step 2: Write minimal implementation**

`bootstrap_configmap.yaml` に dnsmasq 設定を置き、ProxyDHCP と TFTP を有効にする。`bootstrap_deployment.yaml` は `ghcr.io/networkboot/dnsmasq:latest` を使い、hostNetwork と `NET_ADMIN` capability を付ける。HTTP dummy script は manager の `:8082` を想定し、ConfigMap に iPXE chain 先 URL を入れる。

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: tart-bootstrap-dnsmasq
  namespace: tart-system
data:
  dnsmasq.conf: |
    port=0
    log-dhcp
    log-queries
    dhcp-range=192.0.2.0,proxy
    dhcp-no-override
    pxe-service=X86PC,"Tart iPXE",ipxe.efi
    enable-tftp
    tftp-root=/var/lib/tftpboot
    dhcp-boot=tag:!ipxe,ipxe.efi
    dhcp-userclass=set:ipxe,iPXE
    dhcp-boot=tag:ipxe,http://127.0.0.1:8082/ipxe
```

- [ ] **Step 3: Run config verification**

Run: `mise run kustomize build config/bootstrap`
Expected: PASS with Namespace, ConfigMap, Service, Deployment YAML rendered.

- [ ] **Step 4: Commit**

Run: `git --no-pager add config/bootstrap && git --no-pager commit --signoff -m "Add bootstrap dnsmasq manifests"`

### Task 4: ドキュメント更新と全体検証

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Write the failing doc-oriented test**

README に Phase 2 の構成が見当たらないことを確認し、追記対象を固定する。

Run: `rg -n "Phase 2|dnsmasq|iPXE" README.md`
Expected: FAIL or no relevant section describing the new bootstrap components.

- [ ] **Step 2: Write minimal documentation**

README に Phase 2 の最小構成として `config/bootstrap` と `--ipxe-bind-address` を追記する。

```md
## Network boot bootstrap

`config/bootstrap` contains the minimal ProxyDHCP/TFTP manifests for dnsmasq.
The manager exposes a placeholder iPXE script endpoint on `--ipxe-bind-address` and returns a dummy script at `/ipxe`.
```

- [ ] **Step 3: Run full verification**

Run: `mise run test`
Expected: PASS.

Run: `mise run kustomize build config/bootstrap`
Expected: PASS.

- [ ] **Step 4: Commit**

Run: `git --no-pager add README.md docs/superpowers/plans/2026-05-03-phase2-network-boot.md && git --no-pager commit --signoff -m "Document phase2 network boot bootstrap"`
