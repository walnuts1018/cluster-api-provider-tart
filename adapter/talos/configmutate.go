package talos

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/blang/semver/v4"
	"github.com/siderolabs/crypto/x509"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/container"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/rotatepatcher"
	k8sconfig "github.com/siderolabs/talos/pkg/machinery/config/types/k8s"
	configmeta "github.com/siderolabs/talos/pkg/machinery/config/types/meta"
	runtimeconfig "github.com/siderolabs/talos/pkg/machinery/config/types/runtime"
	v1alpha1config "github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
)

// ErrClientUnavailableは接続済みのclientなしでTalos operationが要求されたことを示す。
var (
	ErrClientUnavailable  = errors.New("talos client is unavailable")
	ErrProviderIDConflict = errors.New("talos kubelet provider ID conflicts with the allocated Host")
)

// InstallerImageはdesired Talos image identityに対応するImage Factory installer referenceを返す。
func InstallerImage(version, schematicID string) (string, error) {
	version = strings.TrimSpace(version)
	schematicID = strings.TrimSpace(schematicID)
	if version == "" {
		return "", errors.New("talos image version is empty")
	}
	if !strings.HasPrefix(version, "v") {
		return "", errors.New("talos image version must start with v")
	}
	if _, err := semver.ParseTolerant(version); err != nil {
		return "", fmt.Errorf("parse Talos image version: %w", err)
	}
	if schematicID == "" {
		return "", errors.New("talos image schematic ID is empty")
	}
	for _, character := range schematicID {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '.' && character != '_' && character != '-' {
			return "", errors.New("talos image schematic ID contains an invalid character")
		}
	}

	return fmt.Sprintf("factory.talos.dev/metal-installer/%s:%s", schematicID, version), nil
}

// SetInstallerImageはcomplete machine configuration内のTalos installer imageだけを更新する。documentのmergeとserializationはTalos machineryへ委譲し、既存のdisk、PKI、machine settingを保持する。
func SetInstallerImage(configuration []byte, version, schematicID string) ([]byte, error) {
	if len(bytes.TrimSpace(configuration)) == 0 {
		return nil, errors.New("talos machine configuration is empty")
	}
	image, err := InstallerImage(version, schematicID)
	if err != nil {
		return nil, err
	}

	provider, err := configloader.NewFromBytes(configuration)
	if err != nil {
		return nil, fmt.Errorf("load talos machine configuration: %w", err)
	}
	unattended, ok := provider.UnattendedInstallConfig().(*runtimeconfig.UnattendedInstallConfigV1Alpha1)
	if !ok || unattended == nil {
		return nil, errors.New("talos machine configuration requires an UnattendedInstall configuration")
	}
	patch := unattended.DeepCopy()
	patch.Installer.Image = image
	patchProvider, err := container.New(patch)
	if err != nil {
		return nil, fmt.Errorf("build talos unattended install patch: %w", err)
	}
	output, err := configpatcher.Apply(configpatcher.WithBytes(configuration), []configpatcher.Patch{
		configpatcher.NewStrategicMergePatch(patchProvider),
	})
	if err != nil {
		return nil, fmt.Errorf("patch talos unattended install image: %w", err)
	}
	result, err := output.Bytes()
	if err != nil {
		return nil, fmt.Errorf("encode talos machine configuration: %w", err)
	}
	return result, nil
}

// SetProviderIDはallocation済みTartHostから導出したProviderIDをkubelet configurationへ書き込む。patch適用前に値を確認し、ユーザー所有の競合するProviderIDを黙って置換しない。
func SetProviderID(configuration []byte, providerID string) ([]byte, error) {
	if len(bytes.TrimSpace(configuration)) == 0 {
		return nil, errors.New("talos machine configuration is empty")
	}
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return nil, errors.New("talos provider ID is empty")
	}

	provider, err := configloader.NewFromBytes(configuration)
	if err != nil {
		return nil, fmt.Errorf("load talos machine configuration: %w", err)
	}
	if !provider.Has(k8sconfig.KubeletConfig) {
		return nil, errors.New("talos machine configuration requires a KubeletConfig")
	}
	kubelet := provider.K8sKubeletConfig()
	if kubelet == nil {
		return nil, errors.New("talos machine configuration has no kubelet configuration")
	}
	if values := kubelet.ExtraArgs()["provider-id"]; len(values) > 0 {
		if len(values) != 1 || values[0] != providerID {
			return nil, fmt.Errorf("%w: %q", ErrProviderIDConflict, values[0])
		}
	}

	patch := k8sconfig.NewKubeletConfigV1Alpha1()
	patch.KubeletImage = kubelet.Image()
	patch.KubeletArgs = configmeta.Args{
		"provider-id": configmeta.NewArgValue(providerID, nil),
	}
	patchProvider, err := container.New(patch)
	if err != nil {
		return nil, fmt.Errorf("build talos kubelet provider ID patch: %w", err)
	}
	output, err := configpatcher.Apply(configpatcher.WithBytes(configuration), []configpatcher.Patch{
		configpatcher.NewStrategicMergePatch(patchProvider),
	})
	if err != nil {
		return nil, fmt.Errorf("patch talos kubelet provider ID: %w", err)
	}
	result, err := output.Bytes()
	if err != nil {
		return nil, fmt.Errorf("encode talos machine configuration: %w", err)
	}
	return result, nil
}

// SetMachineCertificateAuthorityはTalos公式のCA rotation手順のうち、machine(OS/apid)のissuing CAとaccepted CA setをTalos machine configuration上で更新する。issuingは常にaccepted集合へ暗黙的に追加されるため、呼び出し側はrotation中に追加で信頼させたい他generationのCAだけをacceptedへ渡す。空のacceptedはissuing以外のCAを信頼しないことを意味し、旧CA削除(rotationの最終段階)に使う。
func SetMachineCertificateAuthority(configuration []byte, issuing *x509.PEMEncodedCertificateAndKey, accepted ...*x509.PEMEncodedCertificateAndKey) ([]byte, error) {
	if len(bytes.TrimSpace(configuration)) == 0 {
		return nil, errors.New("talos machine configuration is empty")
	}
	if issuing == nil {
		return nil, errors.New("talos machine issuing certificate authority is empty")
	}
	provider, err := configloader.NewFromBytes(configuration)
	if err != nil {
		return nil, fmt.Errorf("load talos machine configuration: %w", err)
	}
	machineCA := &x509.PEMEncodedCertificateAndKey{Crt: bytes.Clone(issuing.Crt)}
	if provider.Machine().Type().IsControlPlane() {
		machineCA.Key = bytes.Clone(issuing.Key)
	}
	acceptedCAs := make([]*x509.PEMEncodedCertificate, 0, len(accepted))
	for _, ca := range accepted {
		if ca == nil {
			continue
		}
		acceptedCAs = append(acceptedCAs, &x509.PEMEncodedCertificate{Crt: bytes.Clone(ca.Crt)})
	}
	patched, err := provider.PatchV1Alpha1(func(config *v1alpha1config.Config) error {
		if config.MachineConfig == nil {
			config.MachineConfig = &v1alpha1config.MachineConfig{}
		}
		config.MachineConfig.MachineCA = machineCA
		config.MachineConfig.MachineAcceptedCAs = acceptedCAs
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("patch talos machine certificate authority: %w", err)
	}
	result, err := patched.Bytes()
	if err != nil {
		return nil, fmt.Errorf("encode talos machine configuration: %w", err)
	}
	return result, nil
}

// SetKubernetesAPICertificateAuthorityはKubeAPIServerCAConfig documentのissuing CAとaccepted CA setを更新する。Kubernetes API serverのCA rotationに使う。
func SetKubernetesAPICertificateAuthority(configuration []byte, issuing *x509.PEMEncodedCertificateAndKey, accepted ...*x509.PEMEncodedCertificateAndKey) ([]byte, error) {
	if len(bytes.TrimSpace(configuration)) == 0 {
		return nil, errors.New("talos machine configuration is empty")
	}
	if issuing == nil {
		return nil, errors.New("talos Kubernetes API server issuing certificate authority is empty")
	}
	provider, err := configloader.NewFromBytes(configuration)
	if err != nil {
		return nil, fmt.Errorf("load talos machine configuration: %w", err)
	}
	if !provider.Machine().Type().IsControlPlane() {
		return setWorkerKubernetesAPICertificateAuthority(provider, issuing, accepted...)
	}
	patch := k8sconfig.NewKubeAPIServerCAConfigV1Alpha1()
	patch.APIIssuingCA = &configmeta.CertificateAndKey{Cert: string(issuing.Crt), Key: string(issuing.Key)}
	for _, ca := range accepted {
		if ca == nil {
			continue
		}
		patch.APIAcceptedCAs = append(patch.APIAcceptedCAs, string(ca.Crt))
	}
	patchProvider, err := container.New(patch)
	if err != nil {
		return nil, fmt.Errorf("build talos Kubernetes API server CA patch: %w", err)
	}
	output, err := configpatcher.Apply(configpatcher.WithBytes(configuration), []configpatcher.Patch{
		configpatcher.NewStrategicMergePatch(patchProvider),
	})
	if err != nil {
		return nil, fmt.Errorf("patch talos Kubernetes API server certificate authority: %w", err)
	}
	result, err := output.Bytes()
	if err != nil {
		return nil, fmt.Errorf("encode talos machine configuration: %w", err)
	}
	return result, nil
}

func setWorkerKubernetesAPICertificateAuthority(provider talosconfig.Provider, issuing *x509.PEMEncodedCertificateAndKey, accepted ...*x509.PEMEncodedCertificateAndKey) ([]byte, error) {
	apiConfig := provider.K8sAPIServerCAConfig()
	if apiConfig == nil {
		return nil, errors.New("worker Talos machine configuration has no Kubernetes API server CA")
	}
	desired := make([][]byte, 0, 1+len(accepted))
	desired = append(desired, bytes.Clone(issuing.Crt))
	for _, ca := range accepted {
		if ca != nil && !containsCertificate(desired, ca.Crt) {
			desired = append(desired, bytes.Clone(ca.Crt))
		}
	}
	var err error
	for _, ca := range apiConfig.AcceptedCAs() {
		if ca != nil && !containsCertificate(desired, ca.Crt) {
			provider, err = rotatepatcher.K8sDeleteAcceptedCA(ca.Crt)(provider)
			if err != nil {
				return nil, fmt.Errorf("remove worker Kubernetes API server accepted CA: %w", err)
			}
		}
	}
	provider, err = rotatepatcher.K8sSetCA(issuing)(provider)
	if err != nil {
		return nil, fmt.Errorf("set worker Kubernetes API server accepted CA: %w", err)
	}
	for _, ca := range desired {
		provider, err = rotatepatcher.K8sAddAcceptedCA(ca)(provider)
		if err != nil {
			return nil, fmt.Errorf("add worker Kubernetes API server accepted CA: %w", err)
		}
	}
	result, err := provider.Bytes()
	if err != nil {
		return nil, fmt.Errorf("encode worker Kubernetes API server CA configuration: %w", err)
	}
	return result, nil
}

func containsCertificate(certificates [][]byte, expected []byte) bool {
	for _, certificate := range certificates {
		if bytes.Equal(certificate, expected) {
			return true
		}
	}
	return false
}

// SetKubernetesAggregatorCertificateAuthorityはKubeAggregatorCAConfig documentのissuing CAとaccepted CA setを更新する。Kubernetes API aggregator flowのCA rotationに使う。
func SetKubernetesAggregatorCertificateAuthority(configuration []byte, issuing *x509.PEMEncodedCertificateAndKey, accepted ...*x509.PEMEncodedCertificateAndKey) ([]byte, error) {
	if len(bytes.TrimSpace(configuration)) == 0 {
		return nil, errors.New("talos machine configuration is empty")
	}
	if issuing == nil {
		return nil, errors.New("talos Kubernetes aggregator issuing certificate authority is empty")
	}
	provider, err := configloader.NewFromBytes(configuration)
	if err != nil {
		return nil, fmt.Errorf("load talos machine configuration: %w", err)
	}
	if !provider.Machine().Type().IsControlPlane() {
		return bytes.Clone(configuration), nil
	}
	patch := k8sconfig.NewKubeAggregatorCAConfigV1Alpha1()
	patch.AggregatorIssuingCA = &configmeta.CertificateAndKey{Cert: string(issuing.Crt), Key: string(issuing.Key)}
	for _, ca := range accepted {
		if ca == nil {
			continue
		}
		patch.AggregatorAcceptedCAs = append(patch.AggregatorAcceptedCAs, string(ca.Crt))
	}
	patchProvider, err := container.New(patch)
	if err != nil {
		return nil, fmt.Errorf("build talos Kubernetes aggregator CA patch: %w", err)
	}
	output, err := configpatcher.Apply(configpatcher.WithBytes(configuration), []configpatcher.Patch{
		configpatcher.NewStrategicMergePatch(patchProvider),
	})
	if err != nil {
		return nil, fmt.Errorf("patch talos Kubernetes aggregator certificate authority: %w", err)
	}
	result, err := output.Bytes()
	if err != nil {
		return nil, fmt.Errorf("encode talos machine configuration: %w", err)
	}
	return result, nil
}

// Clientはtalos machineryのgRPC clientを薄くwrapする。Tartのreconcileとpolicy packageが必要とする観測と操作だけを公開する。
