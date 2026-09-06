package talos

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/siderolabs/go-kubernetes/kubernetes/ssa"
	"github.com/siderolabs/go-kubernetes/kubernetes/upgrade"
	talosupstream "github.com/siderolabs/talos/pkg/cluster"
	upstreamkubernetes "github.com/siderolabs/talos/pkg/cluster/kubernetes"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/constants"
)

// Talosのcluster-wide Kubernetes upgradeは単一のgRPC RPCとして公開されておらず、
// talosctlと同じclient-side orchestration(github.com/siderolabs/talos/pkg/cluster/kubernetes)が正本の実装である。
// Tartはこのalgorithmを再実装せず、upstream実装をそのまま呼び出す。
// upstreamへの依存はこのadapterだけに閉じ込め、go.modで machinery module と同じTalos versionへpinして管理する。

// ErrKubernetesUpgradeClientUnavailableは、upgradeへ渡すTalos clientが未接続であることを示す。
var ErrKubernetesUpgradeClientUnavailable = errors.New("talos client for the kubernetes upgrade is unavailable")

// kubernetesUpgradeReconcileTimeoutは、upstream実装がmanifest適用後のreconcileを待つ上限である。
const kubernetesUpgradeReconcileTimeout = 5 * time.Minute

// KubernetesUpgradeRequestはcluster-wide Kubernetes upgradeの入力である。
// versionのsource of truthはTartControlPlane.spec.versionであり、この構造体はそれをupstream実装へ渡すだけの値を持つ。
type KubernetesUpgradeRequest struct {
	// Clientはcontrol-plane nodeへ接続済みのauthenticated Talos clientである。
	Client *Client
	// FromVersionは現在のcluster Kubernetes versionである。空の場合はclusterから検出する。
	FromVersion string
	// ToVersionはdesired Kubernetes versionである。
	ToVersion string
	// ControlPlaneEndpointはworkload Kubernetes APIのendpointであり、Talosが返すkubeconfigのhostを上書きする。
	ControlPlaneEndpoint string
	// LogOutputはupstream実装の進捗出力先である。nilの場合はupstreamの既定(標準出力)になる。
	LogOutput io.Writer
}

// KubernetesUpgradeRunnerはcluster-wide Kubernetes upgradeを実行する境界である。
// 実運用ではTalos upstream実装を呼ぶUpstreamKubernetesUpgradeRunnerを使い、testではfakeへ差し替える。
type KubernetesUpgradeRunner interface {
	// DetectVersionはclusterで動作しているKubernetes componentの最も低いversionを観測する。
	DetectVersion(ctx context.Context, request KubernetesUpgradeRequest) (string, error)
	// Upgradeはcluster-wide Kubernetes upgradeを一度だけ実行する。
	Upgrade(ctx context.Context, request KubernetesUpgradeRequest) error
}

// UpstreamKubernetesUpgradeRunnerはTalos upstreamのpkg/cluster/kubernetes実装へ委譲する。
type UpstreamKubernetesUpgradeRunner struct{}

var _ KubernetesUpgradeRunner = UpstreamKubernetesUpgradeRunner{}

// NormalizeKubernetesVersionは、CAPIが扱う"v"付きversionをTalos upstreamが期待する形式へ揃える。
func NormalizeKubernetesVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

// DetectVersionはupstreamのDetectLowestVersionへ委譲する。
func (UpstreamKubernetesUpgradeRunner) DetectVersion(ctx context.Context, request KubernetesUpgradeRequest) (string, error) {
	provider, options, err := request.upstream()
	if err != nil {
		return "", err
	}
	defer provider.close()

	version, err := upstreamkubernetes.DetectLowestVersion(ctx, provider, options)
	if err != nil {
		return "", fmt.Errorf("detect cluster kubernetes version: %w", err)
	}
	return version, nil
}

// Upgradeはupstreamのk8s.Upgradeへ委譲する。upgrade pathの妥当性判定、component順序、health待ちはupstream実装が持つ責務である。
func (UpstreamKubernetesUpgradeRunner) Upgrade(ctx context.Context, request KubernetesUpgradeRequest) error {
	provider, options, err := request.upstream()
	if err != nil {
		return err
	}
	defer provider.close()

	from := NormalizeKubernetesVersion(request.FromVersion)
	if from == "" {
		from, err = upstreamkubernetes.DetectLowestVersion(ctx, provider, options)
		if err != nil {
			return fmt.Errorf("detect cluster kubernetes version: %w", err)
		}
	}
	path, err := upgrade.NewPath(from, NormalizeKubernetesVersion(request.ToVersion))
	if err != nil {
		return fmt.Errorf("build kubernetes upgrade path: %w", err)
	}
	options.Path = path

	if err := upstreamkubernetes.Upgrade(ctx, provider, options); err != nil {
		return fmt.Errorf("upgrade cluster kubernetes version: %w", err)
	}
	return nil
}

// upgradeProviderはupstreamのUpgradeProviderを、Tartが保持する単一のauthenticated Talos clientから構成する。
type upgradeProvider struct {
	*talosupstream.ConfigClientProvider
	*talosupstream.KubernetesClient
}

func (p *upgradeProvider) close() {
	// clientの寿命は呼び出し側のTart Clientが持つため、ここではupstreamがcacheしたKubernetes clientだけを解放する。
	_ = p.KubernetesClient.K8sClose()
}

// upstreamは、requestからupstream実装が要求するproviderとoptionを組み立てる。
func (r KubernetesUpgradeRequest) upstream() (*upgradeProvider, upstreamkubernetes.UpgradeOptions, error) {
	if r.Client == nil || r.Client.raw == nil {
		return nil, upstreamkubernetes.UpgradeOptions{}, ErrKubernetesUpgradeClientUnavailable
	}
	if NormalizeKubernetesVersion(r.ToVersion) == "" {
		return nil, upstreamkubernetes.UpgradeOptions{}, errors.New("desired kubernetes version is empty")
	}

	clientProvider := &talosupstream.ConfigClientProvider{DefaultClient: r.Client.raw}
	provider := &upgradeProvider{
		ConfigClientProvider: clientProvider,
		KubernetesClient: &talosupstream.KubernetesClient{
			ClientProvider: clientProvider,
			ForceEndpoint:  r.ControlPlaneEndpoint,
		},
	}

	options := upstreamkubernetes.UpgradeOptions{
		ControlPlaneEndpoint: r.ControlPlaneEndpoint,
		LogOutput:            r.LogOutput,
		PrePullImages:        true,
		UpgradeKubelet:       true,
		EncoderOpt:           encoder.WithComments(encoder.CommentsDisabled),

		KubeletImage:           constants.KubeletImage,
		APIServerImage:         constants.KubernetesAPIServerImage,
		ControllerManagerImage: constants.KubernetesControllerManagerImage,
		SchedulerImage:         constants.KubernetesSchedulerImage,
		ProxyImage:             constants.KubeProxyImage,

		InventoryPolicy:  ssa.InventoryPolicyAdoptIfNoInventory,
		ReconcileTimeout: kubernetesUpgradeReconcileTimeout,
	}
	return provider, options, nil
}
