package bootstrap

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

var ErrMachineConfigurationContextIncomplete = errors.New("machine configuration generation context is incomplete")

// MachineConfigurationContextはclusterとCAPI Machineから導出したTalos設定生成の入力である。
// SecretsBundleはKubernetes Secretから検証済みの値だけを渡し、呼び出し側で外部へ出力しない。
type MachineConfigurationContext struct {
	ClusterName          string
	ControlPlaneEndpoint string
	KubernetesVersion    string
	MachineType          machine.Type
	SecretsBundle        *secrets.Bundle
}

// GenerateMachineConfigurationはTalos machineryのbase configurationへユーザーpatchを適用し、
// Talos APIへ渡せるcanonical configurationを返す。installer diskは暗黙に選択せず、必要な場合はpatch側で指定する。
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
	return configuration, nil
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
