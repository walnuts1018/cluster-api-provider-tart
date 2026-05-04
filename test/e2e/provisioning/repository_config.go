package provisioning

import (
	"context"
	"path/filepath"

	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
)

func createClusterctlConfig(ctx context.Context, e2eConfig *clusterctl.E2EConfig, artifactsFolder string) string {
	return clusterctl.CreateRepository(ctx, clusterctl.CreateRepositoryInput{
		E2EConfig:        e2eConfig,
		RepositoryFolder: filepath.Join(artifactsFolder, "repository"),
	})
}
