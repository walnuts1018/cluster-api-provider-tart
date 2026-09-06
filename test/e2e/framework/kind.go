//go:build e2e

// Package frameworkは、Tart E2Eが共通で使うkind cluster構築、CAPI/Tart providerのインストール、
// Condition観測、artifact収集のヘルパーを提供する。CAPI公式test/frameworkは導入せず、
// 本E2Eの範囲(単一management cluster、単一workload cluster)に必要な最小限だけを実装する。
package framework

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	testutils "github.com/walnuts1018/cluster-api-provider-tart/test/utils"
)

const (
	// capiVersionはE2Eで初期化するCluster API coreのversionである。ci.yaml/go.modのCAPI依存
	// (sigs.k8s.io/cluster-api v1.14.1)と揃える。
	capiVersion   = "v1.14.1"
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

// InstallCAPICoreは、clusterctl initでCluster API core(+関連するcert-manager)をkind clusterへ
// installし、Readyになるまで待つ。
//
// 公式のcluster-api-components.yamlはcontainer argsに`${VAR:=default}`形式のvariableを含み、
// clusterctlがこれをsubstituteすることを前提としている。生のkubectl applyでは展開されず、
// managerが起動時flag parseに失敗してCrashLoopBackOffになるため、clusterctlを使う必要がある。
// clusterctl init自体がcert-managerのinstall/待機も行う。
func InstallCAPICore(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "clusterctl", "init", "--core", "cluster-api:"+capiVersion, "--wait-providers")
	if _, err := testutils.Run(cmd); err != nil {
		return fmt.Errorf("clusterctl init: %w", err)
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

// TartProviderDeployments は各providerのDeployment名である。config/default/*/kustomization.yaml
// のnamePrefix(cluster-api-provider-tart-)と、config/manager/*/manager.yamlのDeployment名
// (<role>-controller-manager)を結合した実際の名前と一致させる必要がある。
var TartProviderDeployments = map[string]string{
	"config/default/infrastructure": "cluster-api-provider-tart-infrastructure-controller-manager",
	"config/default/bootstrap":      "cluster-api-provider-tart-bootstrap-controller-manager",
	"config/default/control-plane":  "cluster-api-provider-tart-control-plane-controller-manager",
}

// providerImageは、E2Eがローカルでbuild/loadするimageと、それをkustomize buildの結果へ
// 反映させるために必要な情報を持つ。
type providerImage struct {
	// Nameはローカルでdocker buildする際のimage名(tagなし)であり、kind clusterへloadする
	// 際にも"<Name>:<tag>"として使う。
	Name string
	// KustomizePlaceholderは、このE2E overlayを適用する時点でkustomizeのimages transformerが
	// マッチさせるべき既存のimage名である。通常はNameと同じだが、config/netboot-serverのように
	// 内側のkustomizationで既にnewNameが適用されている場合はその適用後の名前を指定する。
	KustomizePlaceholder string
}

// TartProviderImagesは、config/manager配下のDeployment/Podが参照するimage(kubebuilder scaffold
// 由来の':latest'固定値や、config/netboot-serverのplaceholder registryのように実在しない仮の値)
// を列挙する。E2Eではこれをローカルでbuildしkindへloadしたimageへ置き換える必要がある
// (さもなくば常にImagePullBackOffになる)。
var TartProviderImages = map[string][]providerImage{
	"config/default/infrastructure": {
		{Name: "infrastructure-controller", KustomizePlaceholder: "infrastructure-controller"},
		{Name: "netboot-server", KustomizePlaceholder: "ghcr.io/example/netboot-server"},
	},
	"config/default/bootstrap": {
		{Name: "bootstrap-controller", KustomizePlaceholder: "bootstrap-controller"},
	},
	"config/default/control-plane": {
		{Name: "control-plane-controller", KustomizePlaceholder: "control-plane-controller"},
	},
}

// InstallTartProvidersは、kustomize buildの出力をkubectl applyし、3つのmanager Deploymentが
// Availableになるまで待つ。imageTagが空でない場合、TartProviderImagesの各imageのtagをimageTag
// へ置き換えてからapplyする(LoadProviderImagesForTagで事前にkindへloadしたローカルimageを
// 実際に使わせるため)。
func InstallTartProviders(ctx context.Context, imageTag string) error {
	projectDir, err := testutils.GetProjectDir()
	if err != nil {
		return fmt.Errorf("resolve project directory: %w", err)
	}

	for _, manifestDir := range TartProviderManifests {
		buildTarget := filepath.Join(projectDir, manifestDir)
		if imageTag != "" {
			overlayDir, overlayErr := renderImageOverlay(projectDir, manifestDir, TartProviderImages[manifestDir], imageTag)
			if overlayErr != nil {
				return fmt.Errorf("render image overlay for %q: %w", manifestDir, overlayErr)
			}
			defer os.RemoveAll(overlayDir) //nolint:errcheck // best-effort cleanup of temp overlay
			buildTarget = overlayDir
		}

		// config/crd配下はinfrastructure/bootstrap/control-plane用に分割されており、各々が
		// 兄弟directoryの../bases/*.yamlを参照する(kubebuilder標準の構成)。kustomizeの既定の
		// load restrictorはこの兄弟参照をsecurity違反として拒否するため、明示的に無効化する
		// (参照先はすべてこのrepository内のローカルfileであり、外部/remoteは一切関与しない)。
		buildCmd := exec.CommandContext(ctx, "kustomize", "build", buildTarget, "--load-restrictor", "LoadRestrictionsNone")
		var stderr bytes.Buffer
		buildCmd.Stderr = &stderr
		output, err := buildCmd.Output()
		if err != nil {
			return fmt.Errorf("kustomize build %q: %w: %s", manifestDir, err, stderr.String())
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

// renderImageOverlayは、manifestDir(projectDirからの相対path)をresourceとして参照しつつ、
// 各imageをローカルbuild済みのimage(name:newTag)へ置き換えるkustomization.yamlを、projectDir
// 直下の一時directoryへ書き出す。checked-in fileを直接変更せずに済むよう、独立した一時overlay
// として構成する。projectDir配下に作ることで、絶対pathやsymlink解決の差異
// (例: macOSの/tmp→/private/tmp)に依存しない単純な相対参照("../"+manifestDir)を使える。
func renderImageOverlay(projectDir, manifestDir string, images []providerImage, newTag string) (string, error) {
	dir, err := os.MkdirTemp(projectDir, ".tart-e2e-image-overlay-*")
	if err != nil {
		return "", fmt.Errorf("create temp overlay directory: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n")
	fmt.Fprintf(&sb, "- ../%s\n", manifestDir)
	sb.WriteString("images:\n")
	for _, image := range images {
		fmt.Fprintf(&sb, "- name: %s\n  newName: %s\n  newTag: %s\n", image.KustomizePlaceholder, image.Name, newTag)
	}

	if err := os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte(sb.String()), 0o644); err != nil {
		return "", fmt.Errorf("write overlay kustomization: %w", err)
	}
	return dir, nil
}

// LoadProviderImagesForTagは、TartProviderImagesの各imageについて"<Name>:<imageTag>"という
// 名前でローカルにdocker buildされている前提で、kind clusterへloadする。
func LoadProviderImagesForTag(imageTag string) error {
	loaded := map[string]struct{}{}
	for _, images := range TartProviderImages {
		for _, image := range images {
			ref := image.Name + ":" + imageTag
			if _, ok := loaded[ref]; ok {
				continue
			}
			loaded[ref] = struct{}{}
			if err := testutils.LoadImageToKindClusterWithName(ref); err != nil {
				return fmt.Errorf("load image %q into kind cluster: %w", ref, err)
			}
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
