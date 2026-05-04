package provisioning

import (
	"context"
	"os"
	"path/filepath"

	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	clusterctlv1 "sigs.k8s.io/cluster-api/cmd/clusterctl/api/v1alpha3"
	"sigs.k8s.io/yaml"
)

func createClusterctlConfig(ctx context.Context, e2eConfig *clusterctl.E2EConfig, artifactsFolder string) string {
	repositoryFolder := filepath.Join(artifactsFolder, "repository")

	_ = clusterctl.CreateRepository(ctx, clusterctl.CreateRepositoryInput{
		E2EConfig:        e2eConfig,
		RepositoryFolder: repositoryFolder,
	})

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

	values := map[string]interface{}{
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
