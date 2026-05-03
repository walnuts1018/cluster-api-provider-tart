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

func TestHandlerRejectsNonGET(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/ipxe", nil)
	rec := httptest.NewRecorder()

	ipxe.NewHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandlerServesHealthEndpoints(t *testing.T) {
	for _, path := range []string{"/livez", "/readyz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		ipxe.NewHandler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", path, rec.Code, http.StatusOK)
		}
	}
}

func TestNewServerDisablesLeaderElection(t *testing.T) {
	server := ipxe.NewServer(":8082")

	if server.Addr() != ":8082" {
		t.Fatalf("Addr = %q, want %q", server.Addr(), ":8082")
	}
	if server.NeedLeaderElection() {
		t.Fatal("NeedLeaderElection = true, want false")
	}
}
