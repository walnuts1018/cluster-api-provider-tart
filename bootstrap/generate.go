package bootstrap

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

var (
	ErrMachineConfigurationContextIncomplete = errors.New("machine configuration generation context is incomplete")
	ErrConfigurationConflict                 = errors.New("machine configuration conflicts with provider-owned invariant")
)

// MachineConfigurationContextはclusterとCAPI Machineから導出したTalos設定生成の入力である。
// SecretsBundleはKubernetes Secretから検証済みの値だけを渡し、呼び出し側で外部へ出力しない。
type MachineConfigurationContext struct {
	ClusterName          string
	ControlPlaneEndpoint string
	KubernetesVersion    string
	MachineType          machine.Type
	SecretsBundle        *secrets.Bundle
	// InstallDiskはmaintenance inventoryから選択したinstall対象である。nilの場合、user patchまたはcomplete configurationがinstall targetを含んでいなければならない。
	InstallDisk *InstallDisk
}

// GenerateMachineConfigurationはTalos machineryのbase configurationへユーザーpatchを適用し、Talos APIへ渡せるcanonical configurationを返す。InstallDiskが指定された場合はproviderが選択したinstall targetを付加する。
func GenerateMachineConfiguration(input MachineConfigurationContext, patches ...[]byte) ([]byte, error) {
	if strings.TrimSpace(input.ClusterName) == "" || input.SecretsBundle == nil {
		return nil, ErrMachineConfigurationContextIncomplete
	}
	if input.MachineType != machine.TypeControlPlane && input.MachineType != machine.TypeWorker {
		return nil, fmt.Errorf("%w: unsupported machine type", ErrMachineConfigurationContextIncomplete)
	}

	endpoint, err := canonicalEndpoint(input.ControlPlaneEndpoint)
	if err != nil {
		return nil, err
	}
	kubernetesVersion := strings.TrimPrefix(strings.TrimSpace(input.KubernetesVersion), "v")
	if kubernetesVersion == "" {
		return nil, fmt.Errorf("%w: kubernetes version", ErrMachineConfigurationContextIncomplete)
	}

	generated, err := generate.NewInput(input.ClusterName, endpoint, kubernetesVersion, generate.WithSecretsBundle(input.SecretsBundle))
	if err != nil {
		return nil, fmt.Errorf("create Talos configuration generator: %w", err)
	}
	provider, err := generated.Config(input.MachineType)
	if err != nil {
		return nil, fmt.Errorf("generate Talos machine configuration: %w", err)
	}
	base, err := provider.EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return nil, fmt.Errorf("encode generated Talos machine configuration: %w", err)
	}

	configuration, err := RenderEffectiveConfiguration(base, patches...)
	if err != nil {
		return nil, fmt.Errorf("render generated Talos machine configuration: %w", err)
	}
	if input.InstallDisk != nil {
		configuration, err = EnsureInstallDisk(configuration, *input.InstallDisk)
		if err != nil {
			return nil, fmt.Errorf("set Talos install disk: %w", err)
		}
	}
	effectiveProvider, err := configloader.NewFromBytes(configuration)
	if err != nil {
		return nil, fmt.Errorf("load effective Talos machine configuration: %w", err)
	}
	if err := validateProviderOwnedConfiguration(provider, effectiveProvider, input.ClusterName, endpoint, input.MachineType); err != nil {
		return nil, err
	}
	return configuration, nil
}

func validateProviderOwnedConfiguration(base, effective talosconfig.Provider, clusterName, endpoint string, machineType machine.Type) error {
	if base == nil || effective == nil {
		return fmt.Errorf("%w: configuration provider is unavailable", ErrConfigurationConflict)
	}
	if effective.Machine() == nil || effective.Machine().Type() != machineType {
		return fmt.Errorf("%w: machine type", ErrConfigurationConflict)
	}

	cluster := effective.K8sClusterConfig()
	if cluster == nil || cluster.ClusterName() != clusterName {
		return fmt.Errorf("%w: cluster name", ErrConfigurationConflict)
	}
	actualEndpoint := cluster.ClusterEndpoint()
	if actualEndpoint == nil {
		return fmt.Errorf("%w: cluster endpoint is missing", ErrConfigurationConflict)
	}
	canonicalActualEndpoint, err := canonicalEndpoint(actualEndpoint.String())
	if err != nil || canonicalActualEndpoint != endpoint {
		return fmt.Errorf("%w: cluster endpoint", ErrConfigurationConflict)
	}

	baseImages := componentImages(base)
	effectiveImages := componentImages(effective)
	for index := range baseImages {
		if baseImages[index] != effectiveImages[index] {
			return fmt.Errorf("%w: Kubernetes component image", ErrConfigurationConflict)
		}
	}

	return nil
}

func componentImages(provider talosconfig.Provider) [5]string {
	var images [5]string
	if config := provider.K8sAPIServerConfig(); config != nil {
		images[0] = config.Image()
	}
	if config := provider.K8sControllerManagerConfig(); config != nil {
		images[1] = config.Image()
	}
	if config := provider.K8sSchedulerConfig(); config != nil {
		images[2] = config.Image()
	}
	if config := provider.K8sProxyConfig(); config != nil {
		images[3] = config.Image()
	}
	if config := provider.K8sKubeletConfig(); config != nil {
		images[4] = config.Image()
	}

	return images
}

func canonicalEndpoint(endpoint string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("%w: control-plane endpoint", ErrMachineConfigurationContextIncomplete)
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("%w: invalid control-plane endpoint", ErrMachineConfigurationContextIncomplete)
	}
	parsed.Path = ""
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}
