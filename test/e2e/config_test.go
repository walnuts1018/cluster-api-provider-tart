package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestProvisioningConfigUsesURLSourceForInfrastructureProvider(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join("config", "tart.yaml")
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", configPath, err)
	}

	var config struct {
		Providers []struct {
			Name     string `yaml:"name"`
			Type     string `yaml:"type"`
			Versions []struct {
				Type string `yaml:"type"`
			} `yaml:"versions"`
		} `yaml:"providers"`
	}
	if err := yaml.Unmarshal(configBytes, &config); err != nil {
		t.Fatalf("failed to parse %s: %v", configPath, err)
	}

	for _, provider := range config.Providers {
		if provider.Name != "tart" || provider.Type != "InfrastructureProvider" {
			continue
		}
		if len(provider.Versions) == 0 {
			t.Fatalf("provider %q must define at least one version", provider.Name)
		}
		if provider.Versions[0].Type != "url" {
			t.Fatalf("provider %q must use type %q for rendered install YAML, got %q", provider.Name, "url", provider.Versions[0].Type)
		}
		return
	}

	t.Fatal("tart infrastructure provider not found in e2e config")
}
