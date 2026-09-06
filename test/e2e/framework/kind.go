//go:build e2e

// Package frameworkは、Tart E2Eが共通で使うkind cluster構築、CAPI/Tart providerのインストール、
// Condition観測、artifact収集のヘルパーを提供する。CAPI公式test/frameworkは導入せず、
// 本E2Eの範囲(単一management cluster、単一workload cluster)に必要な最小限だけを実装する。
package framework

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"slices"
	"time"

	testutils "github.com/walnuts1018/cluster-api-provider-tart/test/utils"
)

const (
	// capiVersionはE2Eで初期化するCluster API coreのversionである。ci.yaml/go.modのCAPI依存
	// (sigs.k8s.io/cluster-api v1.14.1)と揃える。
	capiVersion   = "v1.14.1"
	capiCoreURL   = "https://github.com/kubernetes-sigs/cluster-api/releases/download/%s/cluster-api-components.yaml"
	tartNamespace = "cluster-api-provider-tart-system"
)

// KindClusterはE2Eが構築するkind clusterのハンドルである。
type KindCluster struct {
	Name string
}

// CreateKindClusterは新しいkind clusterを作成する。既に同名のclusterが存在する場合は再利用する
// (BeforeSuiteが複数回失敗して再実行された場合でも安全に継続できるようにするため)。
func CreateKindCluster(ctx context.Context, name string) (*KindCluster, error) {
	listOutput, err := testutils.Run(exec.CommandContext(ctx, "kind", "get", "clusters"))
	if err == nil {
		if slices.Contains(testutils.GetNonEmptyLines(listOutput), name) {
			return &KindCluster{Name: name}, nil
		}
	}

	cmd := exec.CommandContext(ctx, "kind", "create", "cluster", "--name", name, "--wait", "5m")
	if _, err := testutils.Run(cmd); err != nil {
		return nil, fmt.Errorf("create kind cluster %q: %w", name, err)
	}
	return &KindCluster{Name: name}, nil
}

// Deleteはkind clusterを削除する。
func (k *KindCluster) Delete(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "kind", "delete", "cluster", "--name", k.Name)
	if _, err := testutils.Run(cmd); err != nil {
		return fmt.Errorf("delete kind cluster %q: %w", k.Name, err)
	}
	return nil
}

// InstallCAPICoreは、Cluster API coreのCRD/controllerをkind clusterへapplyし、Readyになるまで待つ。
func InstallCAPICore(ctx context.Context) error {
	url := fmt.Sprintf(capiCoreURL, capiVersion)
	if _, err := testutils.Run(exec.CommandContext(ctx, "kubectl", "apply", "-f", url)); err != nil {
		return fmt.Errorf("apply cluster-api-components: %w", err)
	}
	return waitDeploymentAvailable(ctx, "capi-system", "capi-controller-manager", 5*time.Minute)
}

// TartProviderManifests は infrastructure/bootstrap/control-plane 3 managerの kustomize entry point
// (config/default配下)を列挙する。3providerは別々のDeployment(bootstrap-manager,
// control-plane-manager, infrastructure-manager)としてapplyする。
var TartProviderManifests = []string{
	"config/default/infrastructure",
	"config/default/bootstrap",
	"config/default/control-plane",
}

// TartProviderDeployments は各providerのDeployment名である(RBAC生成時のroleNameと対応する
// mise.tasks.manifestsのcontroller-gen呼び出しに合わせている)。
var TartProviderDeployments = map[string]string{
	"config/default/infrastructure": "infrastructure-manager",
	"config/default/bootstrap":      "bootstrap-manager",
	"config/default/control-plane":  "control-plane-manager",
}

// InstallTartProvidersは、kustomize buildの出力をkubectl applyし、3つのmanager Deploymentが
// Availableになるまで待つ。
func InstallTartProviders(ctx context.Context) error {
	projectDir, err := testutils.GetProjectDir()
	if err != nil {
		return fmt.Errorf("resolve project directory: %w", err)
	}

	for _, manifestDir := range TartProviderManifests {
		buildCmd := exec.CommandContext(ctx, "kustomize", "build", manifestDir)
		buildCmd.Dir = projectDir
		output, err := buildCmd.Output()
		if err != nil {
			return fmt.Errorf("kustomize build %q: %w", manifestDir, err)
		}

		applyCmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", "-")
		applyCmd.Stdin = bytes.NewReader(output)
		if _, err := testutils.Run(applyCmd); err != nil {
			return fmt.Errorf("kubectl apply %q: %w", manifestDir, err)
		}
	}

	for manifestDir, deploymentName := range TartProviderDeployments {
		if err := waitDeploymentAvailable(ctx, tartNamespace, deploymentName, 5*time.Minute); err != nil {
			return fmt.Errorf("wait for %s (from %s) to become available: %w", deploymentName, manifestDir, err)
		}
	}
	return nil
}

// LoadProviderImagesは、ローカルでbuildしたcontroller-manager imageをkind clusterへloadする。
// CIではE2E実行前にdocker buildしたimageをkustomizeのnewNameで参照させるため、
// providerのmanifestをapplyする前に呼び出す想定である。
func LoadProviderImages(images ...string) error {
	for _, image := range images {
		if err := testutils.LoadImageToKindClusterWithName(image); err != nil {
			return fmt.Errorf("load image %q into kind cluster: %w", image, err)
		}
	}
	return nil
}

func waitDeploymentAvailable(ctx context.Context, namespace, name string, timeout time.Duration) error {
	cmd := exec.CommandContext(ctx, "kubectl", "wait", "deployment/"+name,
		"--for", "condition=Available",
		"--namespace", namespace,
		"--timeout", timeout.String(),
	)
	if _, err := testutils.Run(cmd); err != nil {
		return fmt.Errorf("wait for deployment %s/%s: %w", namespace, name, err)
	}
	return nil
}
