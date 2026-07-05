package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestParseConfigRequiresExplicitPreflightInputs(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--controller-url=https://controller.test.walnuts.dev",
		"--operation-uid=operation-uid",
		"--host-uid=host-uid",
		"--boot-mac-address=00:11:22:33:44:55",
		"--plan-key-id=test-key",
		"--plan-key-file=/trust/plan.pem",
		"--preflight-only",
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if !cfg.preflight || cfg.planKeyID != "test-key" {
		t.Fatalf("parseConfig() = %#v", cfg)
	}
	if _, err := parseConfig([]string{"--controller-url=https://controller.test.walnuts.dev"}); err == nil {
		t.Fatal("parseConfig() accepted missing identity and trust inputs")
	}
}

func TestParseConfigRequiresExactlyOneDiagnosticMode(t *testing.T) {
	base := []string{
		"--controller-url=https://controller.test.walnuts.dev",
		"--operation-uid=operation-uid",
		"--host-uid=host-uid",
		"--boot-mac-address=00:11:22:33:44:55",
		"--plan-key-id=test-key",
		"--plan-key-file=/trust/plan.pem",
	}
	if _, err := parseConfig(base); err == nil {
		t.Fatal("parseConfig() accepted no diagnostic mode")
	}
	both := append(base, "--preflight-only", "--prepare-layout-only")
	if _, err := parseConfig(both); err == nil {
		t.Fatal("parseConfig() accepted both diagnostic modes")
	}
	prepare := append(base, "--prepare-layout-only")
	cfg, err := parseConfig(prepare)
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if !cfg.prepareLayout {
		t.Fatalf("parseConfig() = %#v", cfg)
	}
}

func TestLoadPlanPublicKeyAcceptsOnlyEd25519PKIXPEM(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "plan.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadPlanPublicKey(path)
	if err != nil {
		t.Fatalf("loadPlanPublicKey() error = %v", err)
	}
	if !publicKey.Equal(got) {
		t.Fatal("loadPlanPublicKey() returned another key")
	}
}
