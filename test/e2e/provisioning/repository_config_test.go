package provisioning

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
)

func TestCreateClusterctlConfigReturnsExistingConfigPath(t *testing.T) {
	t.Parallel()
	RegisterTestingT(t)

	tempDir := t.TempDir()
	componentsPath := filepath.Join(tempDir, "components.yaml")
	if err := os.WriteFile(componentsPath, []byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: tart-system\n"), 0o600); err != nil {
		t.Fatalf("failed to write test components: %v", err)
	}

	config := &clusterctl.E2EConfig{
		Providers: []clusterctl.ProviderConfig{
			{
				Name: "cluster-api",
				Type: "CoreProvider",
				Versions: []clusterctl.ProviderVersionSource{
					{
						Name:     "v1.9.4",
						Type:     clusterctl.URLSource,
						Value:    "file://" + componentsPath,
						Contract: "v1beta1",
					},
				},
			},
			{
				Name: "tart",
				Type: "InfrastructureProvider",
				Versions: []clusterctl.ProviderVersionSource{
					{
						Name:  "v0.0.0",
						Type:  clusterctl.URLSource,
						Value: "file://" + componentsPath,
					},
				},
			},
		},
		Variables: map[string]string{
			"KUBERNETES_VERSION": "v1.35.0",
		},
	}

	clusterctlConfigPath := createClusterctlConfig(context.Background(), config, tempDir)
	if _, err := os.Stat(clusterctlConfigPath); err != nil {
		t.Fatalf("expected clusterctl config at %s: %v", clusterctlConfigPath, err)
	}

	expectedPath := filepath.Join(tempDir, "repository", "clusterctl-config.yaml")
	if clusterctlConfigPath != expectedPath {
		t.Fatalf("expected clusterctl config path %s, got %s", expectedPath, clusterctlConfigPath)
	}

	clusterctlConfig, err := os.ReadFile(clusterctlConfigPath)
	if err != nil {
		t.Fatalf("expected clusterctl config at %s: %v", clusterctlConfigPath, err)
	}

	clusterctlConfigText := string(clusterctlConfig)
	if strings.Contains(clusterctlConfigText, "/latest/components.yaml") {
		t.Fatalf("expected version-pinned provider URL, got %s", clusterctlConfigText)
	}

	if !strings.Contains(clusterctlConfigText, filepath.Join("cluster-api", "v1.9.4", "components.yaml")) {
		t.Fatalf("expected cluster-api provider URL to pin v1.9.4, got %s", clusterctlConfigText)
	}
}
