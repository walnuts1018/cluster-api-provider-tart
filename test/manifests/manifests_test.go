package manifests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerManifestEnablesEmbeddedBootstrapServers(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "manager", "manager.yaml"))
	if err != nil {
		t.Fatalf("failed to read manager manifest: %v", err)
	}

	text := string(data)
	for _, want := range []string{
		"hostNetwork: true",
		"dnsPolicy: Default",
		"containerPort: 67",
		"containerPort: 69",
		"protocol: UDP",
		"--bootstrap-advertise-address=$(POD_IP)",
		"- NET_BIND_SERVICE",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("manager manifest missing %q", want)
		}
	}
}

func TestExternalBootstrapOverlayDisablesEmbeddedBootstrapServers(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "external-bootstrap", "kustomization.yaml"))
	if err != nil {
		t.Fatalf("failed to read external bootstrap kustomization: %v", err)
	}

	text := string(data)
	for _, want := range []string{
		"../default",
		"--bootstrap-bind-address=0",
		"--tftp-bind-address=0",
		"/spec/template/spec/hostNetwork",
		"/spec/template/spec/dnsPolicy",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("external bootstrap overlay missing %q", want)
		}
	}
}

func TestWebhookPatchReusesRuntimeExtensionPort(t *testing.T) {
	managerData, err := os.ReadFile(filepath.Join("..", "..", "config", "manager", "manager.yaml"))
	if err != nil {
		t.Fatalf("failed to read manager manifest: %v", err)
	}
	patchData, err := os.ReadFile(filepath.Join("..", "..", "config", "default", "manager_webhook_patch.yaml"))
	if err != nil {
		t.Fatalf("failed to read webhook patch: %v", err)
	}

	if got := strings.Count(string(managerData), "containerPort: 9443"); got != 1 {
		t.Fatalf("manager manifest has %d port 9443 definitions, want 1", got)
	}
	if strings.Contains(string(patchData), "containerPort: 9443") {
		t.Fatal("webhook patch must reuse the existing manager port 9443")
	}
}
