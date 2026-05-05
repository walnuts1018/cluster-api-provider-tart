package provisioning

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/blang/semver/v4"
	clusterctlv1 "sigs.k8s.io/cluster-api/cmd/clusterctl/api/v1alpha3"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	"sigs.k8s.io/yaml"
)

func createClusterctlConfig(ctx context.Context, e2eConfig *clusterctl.E2EConfig, artifactsFolder string) string {
	repositoryFolder := filepath.Join(artifactsFolder, "repository")

	_ = clusterctl.CreateRepository(ctx, clusterctl.CreateRepositoryInput{
		E2EConfig:        e2eConfig,
		RepositoryFolder: repositoryFolder,
	})

	if err := writeProviderMetadataFiles(e2eConfig, repositoryFolder); err != nil {
		panic(err)
	}

	clusterctlConfigPath := filepath.Join(repositoryFolder, "clusterctl-config.yaml")
	if err := writeVersionPinnedClusterctlConfig(e2eConfig, repositoryFolder, clusterctlConfigPath); err != nil {
		panic(err)
	}

	return clusterctlConfigPath
}

type providerConfig struct {
	Name string `json:"name,omitempty"`
	URL  string `json:"url,omitempty"`
	Type string `json:"type,omitempty"`
}

func writeVersionPinnedClusterctlConfig(e2eConfig *clusterctl.E2EConfig, repositoryFolder, clusterctlConfigPath string) error {
	providers := make([]providerConfig, 0, len(e2eConfig.Providers))
	for _, provider := range e2eConfig.Providers {
		if len(provider.Versions) == 0 {
			continue
		}

		providerLabel := clusterctlv1.ManifestLabel(provider.Name, clusterctlv1.ProviderType(provider.Type))
		providers = append(providers, providerConfig{
			Name: provider.Name,
			Type: provider.Type,
			URL:  filepath.Join(repositoryFolder, providerLabel, provider.Versions[0].Name, "components.yaml"),
		})
	}

	values := map[string]any{
		"providers":       providers,
		"overridesFolder": filepath.Join(repositoryFolder, "overrides"),
	}
	for key := range e2eConfig.Variables {
		values[key] = e2eConfig.MustGetVariable(key)
	}

	data, err := yaml.Marshal(values)
	if err != nil {
		return err
	}

	return os.WriteFile(clusterctlConfigPath, data, 0o600)
}

func writeProviderMetadataFiles(e2eConfig *clusterctl.E2EConfig, repositoryFolder string) error {
	for _, provider := range e2eConfig.Providers {
		providerLabel := clusterctlv1.ManifestLabel(provider.Name, clusterctlv1.ProviderType(provider.Type))

		for _, version := range provider.Versions {
			metadata, err := providerMetadataYAML(version)
			if err != nil {
				return err
			}

			versionPath := filepath.Join(repositoryFolder, providerLabel, version.Name)
			if err := os.WriteFile(filepath.Join(versionPath, "metadata.yaml"), metadata, 0o600); err != nil {
				return fmt.Errorf("failed to write metadata for %s/%s: %w", providerLabel, version.Name, err)
			}
		}
	}

	return nil
}

func providerMetadataYAML(version clusterctl.ProviderVersionSource) ([]byte, error) {
	parsedVersion, err := semver.ParseTolerant(version.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to parse provider version %q: %w", version.Name, err)
	}

	contract := strings.TrimSpace(version.Contract)
	if contract == "" {
		contract = "v1beta1"
	}

	metadata := fmt.Sprintf(`apiVersion: clusterctl.cluster.x-k8s.io/v1alpha3
kind: Metadata
releaseSeries:
  - major: %d
    minor: %d
    contract: %s
`, parsedVersion.Major, parsedVersion.Minor, contract)

	return []byte(metadata), nil
}
