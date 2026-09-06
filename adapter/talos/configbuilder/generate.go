package configbuilder

import (
	"bytes"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"github.com/siderolabs/crypto/x509"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	talosmachine "github.com/siderolabs/talos/pkg/machinery/config/machine"

	domainbootstrap "github.com/walnuts1018/cluster-api-provider-tart/domain/bootstrap"
	usecasebootstrap "github.com/walnuts1018/cluster-api-provider-tart/usecase/bootstrap"
)

// machineTypeは、provider-neutralなdomain.MachineRoleをsiderolabs machineryのmachine.Typeへ変換する。
// この変換はsiderolabs型を扱うadapter層だけが行う。
func machineType(role domainbootstrap.MachineRole) (talosmachine.Type, error) {
	switch role {
	case domainbootstrap.MachineRoleControlPlane:
		return talosmachine.TypeControlPlane, nil
	case domainbootstrap.MachineRoleWorker:
		return talosmachine.TypeWorker, nil
	default:
		return 0, fmt.Errorf("%w: unsupported machine role", domainbootstrap.ErrMachineConfigurationContextIncomplete)
	}
}

// secretsBundleは、usecase層が不透明な値として運んだSecretsBundleを、siderolabs machineryの
// secrets.Bundleへ確定させる。この型断定はadapter層だけが行う。
func secretsBundle(input usecasebootstrap.MachineConfigurationContext) (*secrets.Bundle, error) {
	bundle, ok := input.SecretsBundle.(*secrets.Bundle)
	if !ok || bundle == nil {
		return nil, fmt.Errorf("%w: secrets bundle", domainbootstrap.ErrMachineConfigurationContextIncomplete)
	}
	return bundle, nil
}

// GenerateMachineConfigurationはTalos machineryのbase configurationへユーザーpatchを適用し、Talos APIへ渡せるcanonical configurationを返す。InstallDiskが指定された場合はproviderが選択したinstall targetを付加する。
func GenerateMachineConfiguration(input usecasebootstrap.MachineConfigurationContext, patches ...[]byte) ([]byte, error) {
	if strings.TrimSpace(input.ClusterName) == "" {
		return nil, domainbootstrap.ErrMachineConfigurationContextIncomplete
	}
	bundle, err := secretsBundle(input)
	if err != nil {
		return nil, err
	}
	mType, err := machineType(input.MachineRole)
	if err != nil {
		return nil, err
	}

	endpoint, err := canonicalEndpoint(input.ControlPlaneEndpoint)
	if err != nil {
		return nil, err
	}
	kubernetesVersion := strings.TrimPrefix(strings.TrimSpace(input.KubernetesVersion), "v")
	if kubernetesVersion == "" {
		return nil, fmt.Errorf("%w: kubernetes version", domainbootstrap.ErrMachineConfigurationContextIncomplete)
	}

	generated, err := generate.NewInput(input.ClusterName, endpoint, kubernetesVersion, generate.WithSecretsBundle(bundle))
	if err != nil {
		return nil, fmt.Errorf("create Talos configuration generator: %w", err)
	}
	provider, err := generated.Config(mType)
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
	if err := validateProviderOwnedConfiguration(provider, effectiveProvider, input.ClusterName, endpoint, mType); err != nil {
		return nil, err
	}
	if err := ValidateMachineConfiguration(configuration); err != nil {
		return nil, fmt.Errorf("validate generated Talos machine configuration: %w", err)
	}
	return configuration, nil
}

// ValidateProviderOwnedConfigurationはユーザーが指定したcomplete configurationを、同じcluster contextから生成したprovider-owned基準と比較する。value入力がprovider invariantを迂回しないよう、configurationの保存またはTalos APIへの送信前に呼び出す。
func ValidateProviderOwnedConfiguration(configuration []byte, input usecasebootstrap.MachineConfigurationContext) error {
	if len(bytes.TrimSpace(configuration)) == 0 || strings.TrimSpace(input.ClusterName) == "" {
		return fmt.Errorf("%w: configuration context is incomplete", domainbootstrap.ErrConfigurationConflict)
	}
	bundle, err := secretsBundle(input)
	if err != nil {
		return fmt.Errorf("%w: %w", domainbootstrap.ErrConfigurationConflict, err)
	}
	mType, err := machineType(input.MachineRole)
	if err != nil {
		return fmt.Errorf("%w: %w", domainbootstrap.ErrConfigurationConflict, err)
	}
	endpoint, err := canonicalEndpoint(input.ControlPlaneEndpoint)
	if err != nil {
		return fmt.Errorf("%w: control-plane endpoint: %w", domainbootstrap.ErrConfigurationConflict, err)
	}
	kubernetesVersion := strings.TrimPrefix(strings.TrimSpace(input.KubernetesVersion), "v")
	if kubernetesVersion == "" {
		return fmt.Errorf("%w: Kubernetes version is empty", domainbootstrap.ErrConfigurationConflict)
	}
	generation, err := generate.NewInput(input.ClusterName, endpoint, kubernetesVersion, generate.WithSecretsBundle(bundle))
	if err != nil {
		return fmt.Errorf("%w: create provider-owned configuration: %w", domainbootstrap.ErrConfigurationConflict, err)
	}
	base, err := generation.Config(mType)
	if err != nil {
		return fmt.Errorf("%w: generate provider-owned configuration: %w", domainbootstrap.ErrConfigurationConflict, err)
	}
	effective, err := configloader.NewFromBytes(configuration)
	if err != nil {
		return fmt.Errorf("%w: load effective configuration: %w", domainbootstrap.ErrConfigurationConflict, err)
	}
	if input.InstallDisk != nil {
		if err := validateInstallDiskConfiguration(effective, *input.InstallDisk); err != nil {
			return fmt.Errorf("%w: install disk: %w", domainbootstrap.ErrConfigurationConflict, err)
		}
	}
	return validateProviderOwnedConfiguration(base, effective, input.ClusterName, endpoint, mType)
}

func validateProviderOwnedConfiguration(base, effective talosconfig.Provider, clusterName, endpoint string, machineType talosmachine.Type) error {
	if base == nil || effective == nil {
		return fmt.Errorf("%w: configuration provider is unavailable", domainbootstrap.ErrConfigurationConflict)
	}
	if effective.Machine() == nil || effective.Machine().Type() != machineType {
		return fmt.Errorf("%w: machine type", domainbootstrap.ErrConfigurationConflict)
	}
	if base.Machine() == nil || !sameCertificateAndKey(base.Machine().Security().IssuingCA(), effective.Machine().Security().IssuingCA()) || base.Machine().Security().Token() != effective.Machine().Security().Token() {
		return fmt.Errorf("%w: machine PKI or token", domainbootstrap.ErrConfigurationConflict)
	}
	baseCluster := base.Cluster()
	effectiveCluster := effective.Cluster()
	if baseCluster == nil || effectiveCluster == nil || baseCluster.Token().ID() != effectiveCluster.Token().ID() || baseCluster.Token().Secret() != effectiveCluster.Token().Secret() || !sameCertificateAndKey(baseCluster.Etcd().CA(), effectiveCluster.Etcd().CA()) {
		return fmt.Errorf("%w: cluster PKI or token", domainbootstrap.ErrConfigurationConflict)
	}
	if !reflect.DeepEqual(base.K8sAPIServerCAConfig(), effective.K8sAPIServerCAConfig()) || !reflect.DeepEqual(base.K8sAggregatorCAConfig(), effective.K8sAggregatorCAConfig()) || !reflect.DeepEqual(base.K8sServiceAccountConfig(), effective.K8sServiceAccountConfig()) {
		return fmt.Errorf("%w: Kubernetes PKI", domainbootstrap.ErrConfigurationConflict)
	}

	cluster := effective.K8sClusterConfig()
	if cluster == nil || cluster.ClusterName() != clusterName {
		return fmt.Errorf("%w: cluster name", domainbootstrap.ErrConfigurationConflict)
	}
	actualEndpoint := cluster.ClusterEndpoint()
	if actualEndpoint == nil {
		return fmt.Errorf("%w: cluster endpoint is missing", domainbootstrap.ErrConfigurationConflict)
	}
	canonicalActualEndpoint, err := canonicalEndpoint(actualEndpoint.String())
	if err != nil || canonicalActualEndpoint != endpoint {
		return fmt.Errorf("%w: cluster endpoint", domainbootstrap.ErrConfigurationConflict)
	}

	baseImages := componentImages(base)
	effectiveImages := componentImages(effective)
	for index := range baseImages {
		if baseImages[index] != effectiveImages[index] {
			return fmt.Errorf("%w: Kubernetes component image", domainbootstrap.ErrConfigurationConflict)
		}
	}
	return nil
}

func sameCertificateAndKey(left, right *x509.PEMEncodedCertificateAndKey) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return bytes.Equal(left.Crt, right.Crt) && bytes.Equal(left.Key, right.Key)
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
		return "", fmt.Errorf("%w: control-plane endpoint", domainbootstrap.ErrMachineConfigurationContextIncomplete)
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("%w: invalid control-plane endpoint", domainbootstrap.ErrMachineConfigurationContextIncomplete)
	}
	parsed.Path = ""
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}
