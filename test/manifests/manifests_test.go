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
