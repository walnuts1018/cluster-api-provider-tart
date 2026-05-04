package e2e

import (
	"os"
	"strings"
	"testing"
)

func TestSetupTestProvisioningK3sUsesSingleProxyDHCPRange(t *testing.T) {
	t.Parallel()

	miseToml, err := os.ReadFile("../../mise.toml")
	if err != nil {
		t.Fatalf("failed to read mise.toml: %v", err)
	}

	taskBody := string(miseToml)
	if !strings.Contains(taskBody, "[tasks.setup-test-provisioning-k3s]") {
		t.Fatal("setup-test-provisioning-k3s task not found")
	}

	if strings.Count(taskBody, "--dhcp-range=") != 1 {
		t.Fatalf("expected exactly one dnsmasq dhcp-range option in setup-test-provisioning-k3s task, got %d", strings.Count(taskBody, "--dhcp-range="))
	}

	if !strings.Contains(taskBody, "--dhcp-range=192.168.100.0,proxy") {
		t.Fatal("expected ProxyDHCP range for dnsmasq setup task")
	}
}
