// Package talosはTartが必要とする観測と操作だけをTalos machineryのgRPCクライアントへ適合させ、生成されたTalos API型をcontrollerやpolicy packageへ漏らさない。詳細は.agents/skills/talos/SKILL.mdを参照する。
package talos

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strings"
	"uuid"

	"github.com/blang/semver/v4"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/crypto/x509"
	common "github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/compatibility"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/container"
	k8sconfig "github.com/siderolabs/talos/pkg/machinery/config/types/k8s"
	configmeta "github.com/siderolabs/talos/pkg/machinery/config/types/meta"
	runtimeconfig "github.com/siderolabs/talos/pkg/machinery/config/types/runtime"
	v1alpha1config "github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
	configresource "github.com/siderolabs/talos/pkg/machinery/resources/config"
	machineryhardware "github.com/siderolabs/talos/pkg/machinery/resources/hardware"
	machinerynetwork "github.com/siderolabs/talos/pkg/machinery/resources/network"
	machineryruntime "github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
	"google.golang.org/protobuf/types/known/emptypb"

	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
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
	if provider.UnattendedInstallConfig() != nil {
		unattended, ok := provider.UnattendedInstallConfig().(*runtimeconfig.UnattendedInstallConfigV1Alpha1)
		if !ok {
			return nil, errors.New("talos unattended install configuration has an unsupported type")
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
	if provider.Machine() == nil {
		return nil, errors.New("talos machine configuration has no install configuration")
	}

	patched, err := provider.PatchV1Alpha1(func(config *v1alpha1config.Config) error {
		if config.MachineConfig == nil {
			config.MachineConfig = &v1alpha1config.MachineConfig{}
		}
		if config.MachineConfig.MachineInstall == nil { //nolint:staticcheck // 旧Talos設定形式を扱うため。
			config.MachineConfig.MachineInstall = &v1alpha1config.InstallConfig{} //nolint:staticcheck // 旧Talos設定形式を扱うため。
		}
		config.MachineConfig.MachineInstall.InstallImage = image //nolint:staticcheck // 旧Talos設定形式を扱うため。

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("patch talos install image: %w", err)
	}
	result, err := patched.Bytes()
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
	kubelet := provider.K8sKubeletConfig()
	if kubelet == nil {
		return nil, errors.New("talos machine configuration has no kubelet configuration")
	}
	if values := kubelet.ExtraArgs()["provider-id"]; len(values) > 0 && values[0] != providerID {
		return nil, fmt.Errorf("%w: %q", ErrProviderIDConflict, values[0])
	}

	if provider.Has(k8sconfig.KubeletConfig) {
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

	patched, err := provider.PatchV1Alpha1(func(config *v1alpha1config.Config) error {
		if config.MachineConfig == nil {
			config.MachineConfig = &v1alpha1config.MachineConfig{}
		}
		if config.MachineConfig.MachineKubelet == nil { //nolint:staticcheck // 旧Talos設定形式を扱うため。
			config.MachineConfig.MachineKubelet = &v1alpha1config.KubeletConfig{} //nolint:staticcheck // 旧Talos設定形式を扱うため。
		}
		if config.MachineConfig.MachineKubelet.KubeletExtraArgs == nil { //nolint:staticcheck // 旧Talos設定形式を扱うため。
			config.MachineConfig.MachineKubelet.KubeletExtraArgs = configmeta.Args{} //nolint:staticcheck // 旧Talos設定形式を扱うため。
		}
		config.MachineConfig.MachineKubelet.KubeletExtraArgs["provider-id"] = configmeta.NewArgValue(providerID, nil) //nolint:staticcheck // 旧Talos設定形式を扱うため。

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("patch legacy Talos kubelet provider ID: %w", err)
	}
	result, err := patched.Bytes()
	if err != nil {
		return nil, fmt.Errorf("encode Talos machine configuration: %w", err)
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
		config.MachineConfig.MachineCA = &x509.PEMEncodedCertificateAndKey{Crt: bytes.Clone(issuing.Crt), Key: bytes.Clone(issuing.Key)}
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
//
//nolint:dupl // SetKubernetesAggregatorCertificateAuthorityと構造は同じだが、操作対象のTalos config document型が異なるため共通化しない。
func SetKubernetesAPICertificateAuthority(configuration []byte, issuing *x509.PEMEncodedCertificateAndKey, accepted ...*x509.PEMEncodedCertificateAndKey) ([]byte, error) {
	if len(bytes.TrimSpace(configuration)) == 0 {
		return nil, errors.New("talos machine configuration is empty")
	}
	if issuing == nil {
		return nil, errors.New("talos Kubernetes API server issuing certificate authority is empty")
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

// SetKubernetesAggregatorCertificateAuthorityはKubeAggregatorCAConfig documentのissuing CAとaccepted CA setを更新する。Kubernetes API aggregator flowのCA rotationに使う。
//
//nolint:dupl // SetKubernetesAPICertificateAuthorityと構造は同じだが、操作対象のTalos config document型が異なるため共通化しない。
func SetKubernetesAggregatorCertificateAuthority(configuration []byte, issuing *x509.PEMEncodedCertificateAndKey, accepted ...*x509.PEMEncodedCertificateAndKey) ([]byte, error) {
	if len(bytes.TrimSpace(configuration)) == 0 {
		return nil, errors.New("talos machine configuration is empty")
	}
	if issuing == nil {
		return nil, errors.New("talos Kubernetes aggregator issuing certificate authority is empty")
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
type Client struct {
	raw *talosclient.Client
}

// Closeは内部のgRPC connectionを解放する。
func (c *Client) Close() error {
	if c == nil || c.raw == nil {
		return nil
	}
	return c.raw.Close()
}

// Versionはmachineのauthenticatedまたはmaintenance APIが返したTalos OS versionとplatformの観測値である。
type Version struct {
	Tag      string
	SHA      string
	Platform string
	Arch     string
}

// Versionは接続中のnodeからTalos OS versionの観測値を取得する。reconcileの呼び出し側は結果をdesired imageと比較し、不一致ならMachineをunreadyのままにする。
func (c *Client) Version(ctx context.Context) (Version, error) {
	if c == nil || c.raw == nil {
		return Version{}, ErrClientUnavailable
	}

	resp, err := c.raw.Version(ctx)
	if err != nil {
		return Version{}, fmt.Errorf("get talos version: %w", err)
	}
	messages := resp.GetMessages()
	if len(messages) == 0 {
		return Version{}, fmt.Errorf("get talos version: empty response")
	}
	v := messages[0].GetVersion()
	return Version{
		Tag:      v.GetTag(),
		SHA:      v.GetSha(),
		Platform: messages[0].GetPlatform().GetName(),
		Arch:     v.GetArch(),
	}, nil
}

// ValidateUpgradeはTalos公式のversion compatibility判定へ委譲し、直接のdowngradeや未対応minorへの更新を拒否する。
func ValidateUpgrade(current, desired string) error {
	current = strings.TrimSpace(current)
	desired = strings.TrimSpace(desired)
	if current == "" || desired == "" {
		return errors.New("talos upgrade versions are required")
	}
	currentSemanticVersion, err := semver.ParseTolerant(current)
	if err != nil {
		return fmt.Errorf("parse current Talos semantic version: %w", err)
	}
	desiredSemanticVersion, err := semver.ParseTolerant(desired)
	if err != nil {
		return fmt.Errorf("parse desired Talos semantic version: %w", err)
	}
	if desiredSemanticVersion.LT(currentSemanticVersion) {
		return fmt.Errorf("talos downgrade from %s to %s is not supported", current, desired)
	}
	if desiredSemanticVersion.EQ(currentSemanticVersion) {
		return nil
	}
	minimumLifecycleVersion, err := semver.Parse("1.13.0")
	if err != nil {
		return fmt.Errorf("parse minimum Talos Lifecycle version: %w", err)
	}
	if currentSemanticVersion.LT(minimumLifecycleVersion) {
		return fmt.Errorf("talos upgrade requires Lifecycle API support from v1.13.0; current version is %s", current)
	}
	currentVersion, err := compatibility.ParseTalosVersion(&machine.VersionInfo{Tag: current})
	if err != nil {
		return fmt.Errorf("parse current Talos version: %w", err)
	}
	desiredVersion, err := compatibility.ParseTalosVersion(&machine.VersionInfo{Tag: desired})
	if err != nil {
		return fmt.Errorf("parse desired Talos version: %w", err)
	}
	return desiredVersion.UpgradeableFrom(currentVersion)
}

// ApplyConfigurationはcomplete Talos machine configurationをTalos APIへ渡す。TalosはconfigurationのUnattendedInstallConfigまたはnative設定に従ってinstallationとrebootを実行する。
func (c *Client) ApplyConfiguration(ctx context.Context, configuration []byte) error {
	if c == nil || c.raw == nil {
		return ErrClientUnavailable
	}
	if len(configuration) == 0 {
		return errors.New("talos machine configuration is empty")
	}
	if _, err := c.raw.ApplyConfiguration(ctx, &machine.ApplyConfigurationRequest{
		Data: configuration,
		Mode: machine.ApplyConfigurationRequest_AUTO,
	}); err != nil {
		return fmt.Errorf("apply talos machine configuration: %w", err)
	}
	return nil
}

// ApplyConfigurationLiveは、稼働中nodeへrebootなしでmachine configurationを適用する。ユーザーがLive policyを明示した場合だけ使い、
// 失敗してもrebootを伴う適用へ自動fallbackしない(fallbackするかどうかは呼び出し側のpolicy判断であり、この層では行わない)。
func (c *Client) ApplyConfigurationLive(ctx context.Context, configuration []byte) error {
	return c.applyConfiguration(ctx, configuration, machine.ApplyConfigurationRequest_NO_REBOOT)
}

// RebootはTalos nodeのgraceful rebootを要求する。installationとdataは保持され、同一nodeが起動し直す。
// Talos 1.14ではApplyConfigurationのREBOOT modeが廃止されているため、rebootを伴う適用はapplyとこのrebootの組み合わせで行う。
func (c *Client) Reboot(ctx context.Context) error {
	if c == nil || c.raw == nil {
		return ErrClientUnavailable
	}
	if err := c.raw.Reboot(ctx); err != nil {
		return fmt.Errorf("reboot talos node: %w", err)
	}
	return nil
}

func (c *Client) applyConfiguration(ctx context.Context, configuration []byte, mode machine.ApplyConfigurationRequest_Mode) error {
	if c == nil || c.raw == nil {
		return ErrClientUnavailable
	}
	if len(configuration) == 0 {
		return errors.New("talos machine configuration is empty")
	}
	if _, err := c.raw.ApplyConfiguration(ctx, &machine.ApplyConfigurationRequest{
		Data: configuration,
		Mode: mode,
	}); err != nil {
		return fmt.Errorf("apply talos machine configuration: %w", err)
	}
	return nil
}

// BootTimeは稼働中nodeのboot時刻(Unix秒)を観測する。値の変化はnodeが実際に再起動したことの観測根拠になる。
func (c *Client) BootTime(ctx context.Context) (uint64, error) {
	if c == nil || c.raw == nil {
		return 0, ErrClientUnavailable
	}
	response, err := c.raw.MachineClient.SystemStat(ctx, &emptypb.Empty{})
	if err != nil {
		return 0, fmt.Errorf("get talos system stat: %w", err)
	}
	for _, message := range response.GetMessages() {
		if bootTime := message.GetBootTime(); bootTime != 0 {
			return bootTime, nil
		}
	}
	return 0, errors.New("talos system stat does not report a boot time")
}

// ServicesHealthyは、Talosが管理するserviceのうちhealth checkを持つものが全てhealthyであることを確認する。
// update後にnodeが回復したことをTalos側から観測するために使う。
func (c *Client) ServicesHealthy(ctx context.Context) error {
	if c == nil || c.raw == nil {
		return ErrClientUnavailable
	}
	response, err := c.raw.ServiceList(ctx)
	if err != nil {
		return fmt.Errorf("list talos services: %w", err)
	}
	observed := false
	for _, message := range response.GetMessages() {
		for _, service := range message.GetServices() {
			observed = true
			health := service.GetHealth()
			if health == nil || health.GetUnknown() {
				continue
			}
			if !health.GetHealthy() {
				return fmt.Errorf("talos service %s is not healthy", service.GetId())
			}
		}
	}
	if !observed {
		return errors.New("talos does not report any service state")
	}
	return nil
}

// UpgradeはTalos公式のLifecycle APIへdesired installer imageを渡し、upgrade完了のexit statusを確認する。既存データの保持と再起動はTalosのupgrade semanticsへ委譲する。
func (c *Client) Upgrade(ctx context.Context, image string) error {
	if c == nil || c.raw == nil {
		return ErrClientUnavailable
	}
	image = strings.TrimSpace(image)
	if image == "" {
		return errors.New("talos upgrade image is empty")
	}
	stream, err := c.raw.LifecycleClient.Upgrade(ctx, &machine.LifecycleServiceUpgradeRequest{
		Containerd: &common.ContainerdInstance{
			Driver:    common.ContainerDriver_CRI,
			Namespace: common.ContainerdNamespace_NS_SYSTEM,
		},
		Source: &machine.InstallArtifactsSource{ImageName: image},
	})
	if err != nil {
		return fmt.Errorf("upgrade Talos OS: %w", err)
	}
	exitStatusObserved := false
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("receive Talos OS upgrade progress: %w", err)
		}
		progress := response.GetProgress()
		if progress == nil {
			continue
		}
		if exitCode, ok := progress.GetResponse().(*machine.LifecycleServiceInstallProgress_ExitCode); ok {
			exitStatusObserved = true
			if exitCode.ExitCode != 0 {
				return fmt.Errorf("upgrade Talos OS exited with code %d", exitCode.ExitCode)
			}
		}
	}
	if !exitStatusObserved {
		return errors.New("upgrade Talos OS ended without an exit status")
	}
	return nil
}

// BootstrapはTalos control-plane etcd bootstrap operationを開始する。呼び出し側はauthenticated control-plane machineへ到達できることを確認してから呼び出す。
func (c *Client) Bootstrap(ctx context.Context) error {
	if c == nil || c.raw == nil {
		return ErrClientUnavailable
	}
	if err := c.raw.Bootstrap(ctx, &machine.BootstrapRequest{}); err != nil {
		return fmt.Errorf("bootstrap talos control plane: %w", err)
	}
	return nil
}

// EtcdStatusはOS installationだけを完了したmachineと稼働中のcontrol-plane memberを区別するために必要なetcd観測値である。
type EtcdStatus struct {
	MemberID uint64
	Leader   uint64
	Errors   []string
}

// EtcdMemberはTalosから観測したetcd memberの非機密identityである。
type EtcdMember struct {
	ID       uint64
	Hostname string
	PeerURLs []string
	Learner  bool
}

// EtcdStatusはauthenticated Talos APIを通じてローカルetcd memberを観測する。
func (c *Client) EtcdStatus(ctx context.Context) (EtcdStatus, error) {
	if c == nil || c.raw == nil {
		return EtcdStatus{}, ErrClientUnavailable
	}
	response, err := c.raw.EtcdStatus(ctx)
	if err != nil {
		return EtcdStatus{}, fmt.Errorf("get talos etcd status: %w", err)
	}
	messages := response.GetMessages()
	if len(messages) == 0 || messages[0].GetMemberStatus() == nil {
		return EtcdStatus{}, errors.New("get talos etcd status: empty response")
	}
	status := messages[0].GetMemberStatus()
	return EtcdStatus{
		MemberID: status.GetMemberId(),
		Leader:   status.GetLeader(),
		Errors:   append([]string(nil), status.GetErrors()...),
	}, nil
}

// EtcdMembersはauthenticated Talos APIからetcdの現在のmember集合を取得する。member removal前のquorum判定とremove後の完了確認に使用する。
func (c *Client) EtcdMembers(ctx context.Context) ([]EtcdMember, error) {
	if c == nil || c.raw == nil {
		return nil, ErrClientUnavailable
	}
	response, err := c.raw.EtcdMemberList(ctx, &machine.EtcdMemberListRequest{})
	if err != nil {
		return nil, fmt.Errorf("list talos etcd members: %w", err)
	}
	messages := response.GetMessages()
	if len(messages) == 0 {
		return nil, errors.New("list talos etcd members: empty response")
	}
	if len(messages[0].GetMembers()) == 0 {
		return nil, errors.New("list talos etcd members: membership is empty")
	}
	members := make([]EtcdMember, 0, len(messages[0].GetMembers()))
	seen := make(map[uint64]struct{}, len(messages[0].GetMembers()))
	for _, member := range messages[0].GetMembers() {
		if member == nil || member.GetId() == 0 {
			return nil, errors.New("list talos etcd members: member identity is invalid")
		}
		if _, exists := seen[member.GetId()]; exists {
			return nil, errors.New("list talos etcd members: duplicate member identity")
		}
		seen[member.GetId()] = struct{}{}
		members = append(members, EtcdMember{
			ID:       member.GetId(),
			Hostname: member.GetHostname(),
			PeerURLs: append([]string(nil), member.GetPeerUrls()...),
			Learner:  member.GetIsLearner(),
		})
	}
	return members, nil
}

// RemoveEtcdMemberはTalos公式APIへmember IDによるetcd member removalを委譲する。quorum維持の判定は呼び出し側がremove前に完了させる。
func (c *Client) RemoveEtcdMember(ctx context.Context, memberID uint64) error {
	if c == nil || c.raw == nil {
		return ErrClientUnavailable
	}
	if memberID == 0 {
		return errors.New("talos etcd member ID is empty")
	}
	if err := c.raw.EtcdRemoveMemberByID(ctx, &machine.EtcdRemoveMemberByIDRequest{MemberId: memberID}); err != nil {
		return fmt.Errorf("remove Talos etcd member: %w", err)
	}
	return nil
}

// Kubeconfigはauthenticated Talos APIからworkload clusterのkubeconfigを返す。呼び出し側はbytesをメモリ内だけで保持し、Status、Event、log、metricsへ出力しない。
func (c *Client) Kubeconfig(ctx context.Context) ([]byte, error) {
	if c == nil || c.raw == nil {
		return nil, ErrClientUnavailable
	}
	configuration, err := c.raw.Kubeconfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("get workload kubeconfig from talos: %w", err)
	}
	return configuration, nil
}

// InventoryはTalos maintenance APIを通じて観測したstable hardware identityを含む。呼び出し側にはTalos resource typeを公開しない。
type Inventory struct {
	BootID            string
	SystemUUID        uuid.UUID
	Architecture      string
	MACAddresses      []network.MACAddress
	Disks             []DiskInventory
	NetworkInterfaces []NetworkInterfaceInventory
}

// DiskInventoryはTalosから観測した非機密のdisk情報である。
type DiskInventory struct {
	DevicePath string
	SizeBytes  uint64
	Model      string
	Serial     string
	WWID       string
	BusPath    string
	Transport  string
	Rotational bool
	ReadOnly   bool
	Symlinks   []string
}

// NetworkInterfaceInventoryはTalosから観測した非機密の物理NIC情報である。
type NetworkInterfaceInventory struct {
	Name       string
	MACAddress network.MACAddress
	LinkState  string
	Driver     string
	BusPath    string
	Addresses  []string
}

// HasMACは観測した物理linkにexpected Host enrollment identityが含まれるかを返す。
func (i Inventory) HasMAC(expected network.MACAddress) bool {
	if expected.IsZero() {
		return false
	}
	return slices.Contains(i.MACAddresses, expected)
}

// Inventoryは認証前に取得可能なhardware identityを読み取る。MAC addressを使ってconfigured endpointをclaimed TartHostへbindする。
func (c *Client) Inventory(ctx context.Context) (Inventory, error) {
	if c == nil || c.raw == nil {
		return Inventory{}, ErrClientUnavailable
	}
	links, err := safe.ReaderListAll[*machinerynetwork.LinkStatus](ctx, c.raw.COSI)
	if err != nil {
		return Inventory{}, fmt.Errorf("list talos network links: %w", err)
	}

	observed := Inventory{}
	if bootID, bootIDErr := safe.ReaderGetByID[*machineryruntime.BootID](ctx, c.raw.COSI, machineryruntime.BootIDID); bootIDErr == nil {
		observed.BootID = strings.TrimSpace(bootID.TypedSpec().BootID)
	}
	addressesByLink := make(map[string][]string)
	addressStatuses, addressErr := safe.ReaderListAll[*machinerynetwork.AddressStatus](ctx, c.raw.COSI)
	if addressErr == nil {
		for address := range addressStatuses.All() {
			spec := address.TypedSpec()
			if spec.Address.IsValid() && spec.LinkName != "" {
				addressesByLink[spec.LinkName] = append(addressesByLink[spec.LinkName], spec.Address.String())
			}
		}
	}
	for link := range links.All() {
		spec := link.TypedSpec()
		if !spec.Physical() {
			continue
		}
		for _, address := range []net.HardwareAddr{
			net.HardwareAddr(spec.HardwareAddr),
			net.HardwareAddr(spec.PermanentAddr),
		} {
			if len(address) != 0 {
				macAddress, parseErr := network.ParseMACAddress(address.String())
				if parseErr != nil {
					return Inventory{}, fmt.Errorf("parse Talos network MAC address: %w", parseErr)
				}
				observed.MACAddresses = append(observed.MACAddresses, macAddress)
			}
		}

		linkName := link.Metadata().ID()
		linkState := "down"
		if spec.LinkState {
			linkState = "up"
		}
		macAddress, parseErr := parseHardwareAddress(spec.HardwareAddr)
		if parseErr != nil {
			return Inventory{}, fmt.Errorf("parse Talos network interface MAC address: %w", parseErr)
		}
		if macAddress.IsZero() {
			macAddress, parseErr = parseHardwareAddress(spec.PermanentAddr)
			if parseErr != nil {
				return Inventory{}, fmt.Errorf("parse Talos permanent MAC address: %w", parseErr)
			}
		}
		observed.NetworkInterfaces = append(observed.NetworkInterfaces, NetworkInterfaceInventory{
			Name:       linkName,
			MACAddress: macAddress,
			LinkState:  linkState,
			Driver:     spec.Driver,
			BusPath:    spec.BusPath,
			Addresses:  append([]string(nil), addressesByLink[linkName]...),
		})
	}
	if len(observed.MACAddresses) == 0 {
		return Inventory{}, errors.New("talos maintenance inventory has no physical MAC address")
	}

	systems, err := safe.ReaderListAll[*machineryhardware.SystemInformation](ctx, c.raw.COSI)
	if err == nil {
		for system := range systems.All() {
			if uuidValue := strings.TrimSpace(system.TypedSpec().UUID); uuidValue != "" {
				systemUUID, parseErr := uuid.Parse(uuidValue)
				if parseErr != nil {
					return Inventory{}, fmt.Errorf("parse Talos system UUID: %w", parseErr)
				}
				observed.SystemUUID = systemUUID
				break
			}
		}
	}
	if version, err := c.Version(ctx); err == nil {
		observed.Architecture = version.Arch
	}
	if disks, err := safe.ReaderListAll[*block.Disk](ctx, c.raw.COSI); err == nil {
		for disk := range disks.All() {
			spec := disk.TypedSpec()
			observed.Disks = append(observed.Disks, DiskInventory{
				DevicePath: spec.DevPath,
				SizeBytes:  spec.Size,
				Model:      spec.Model,
				Serial:     spec.Serial,
				WWID:       spec.WWID,
				BusPath:    spec.BusPath,
				Transport:  spec.Transport,
				Rotational: spec.Rotational,
				ReadOnly:   spec.Readonly,
				Symlinks:   append([]string(nil), spec.Symlinks...),
			})
		}
	}

	slices.SortFunc(observed.MACAddresses, func(left, right network.MACAddress) int {
		return strings.Compare(left.String(), right.String())
	})
	observed.MACAddresses = uniqueMACAddresses(observed.MACAddresses)
	slices.SortFunc(observed.NetworkInterfaces, func(left, right NetworkInterfaceInventory) int {
		return strings.Compare(left.Name, right.Name)
	})
	for index := range observed.NetworkInterfaces {
		slices.Sort(observed.NetworkInterfaces[index].Addresses)
		observed.NetworkInterfaces[index].Addresses = uniqueStrings(observed.NetworkInterfaces[index].Addresses)
	}
	slices.SortFunc(observed.Disks, func(left, right DiskInventory) int {
		if comparison := strings.Compare(left.DevicePath, right.DevicePath); comparison != 0 {
			return comparison
		}
		if comparison := strings.Compare(left.Serial, right.Serial); comparison != 0 {
			return comparison
		}
		return strings.Compare(left.WWID, right.WWID)
	})

	return observed, nil
}

// ActiveMachineConfigurationはauthenticated Talos APIを通じて、稼働中nodeへ現在適用されているmachine configurationを観測する。CA rotationの進行段階判定やin-place updateの安全な差分評価は、この観測値をprovider-owned desired configurationの基準とする。
func (c *Client) ActiveMachineConfiguration(ctx context.Context) ([]byte, error) {
	if c == nil || c.raw == nil {
		return nil, ErrClientUnavailable
	}
	resource, err := safe.ReaderGetByID[*configresource.MachineConfig](ctx, c.raw.COSI, configresource.ActiveID)
	if err != nil {
		return nil, fmt.Errorf("get talos active machine configuration: %w", err)
	}
	provider := resource.Provider()
	if provider == nil {
		return nil, errors.New("get talos active machine configuration: provider is unavailable")
	}
	data, err := provider.Bytes()
	if err != nil {
		return nil, fmt.Errorf("encode talos active machine configuration: %w", err)
	}
	return data, nil
}

// SchematicIDはImage Factoryがnodeへ注入したsystem extension setのidentityを観測する。観測できない場合はdesired schematicとの一致を証明できないためerrorを返す。
func (c *Client) SchematicID(ctx context.Context) (string, error) {
	if c == nil || c.raw == nil {
		return "", ErrClientUnavailable
	}
	resource, err := safe.ReaderGetByID[*machineryruntime.ImageFactorySchematic](ctx, c.raw.COSI, machineryruntime.ImageFactorySchematicID)
	if err != nil {
		return "", fmt.Errorf("get Talos Image Factory schematic: %w", err)
	}
	schematicID := strings.TrimSpace(resource.TypedSpec().SchematicID)
	if schematicID == "" {
		return "", errors.New("get Talos Image Factory schematic: schematic ID is unavailable")
	}
	return schematicID, nil
}

func parseHardwareAddress(value []byte) (network.MACAddress, error) {
	if len(value) == 0 {
		return network.MACAddress{}, nil
	}
	return network.ParseMACAddress(net.HardwareAddr(value).String())
}

func uniqueMACAddresses(values []network.MACAddress) []network.MACAddress {
	if len(values) < 2 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}

// ResetはTalos machine APIのReset RPCを呼び出し、system diskを消去してmaintenance modeへ戻す。
// これは不可逆にデータを破棄する操作であるため、呼び出し側は必ず認証済み接続(DialAuthenticatedまたは
// DialAuthenticatedFromConfiguration/FromBundleが確立したClient)に対してのみこのメソッドを呼び出さなければならない。
// DialMaintenanceで確立した無認証接続に対してResetを呼び出すことは、対象Hostの正当性を何も証明していないため
// 絶対に行ってはならない(呼び出し側の責務。本メソッド自体は接続の認証状態を検証しない)。
// Graceful/Rebootは常にtrueとし、control-plane machineの場合はetcdからの離脱を試みたうえでmaintenance modeへ
// 再起動する最も安全な選択にする。
//
// Reset scope: SystemPartitionsToWipeとUserDisksToWipeを空のまま明示し、WipeModeへ明示的にALLを渡す。
// これはTalos自身のsystem installation(system disk上のSTATE/EPHEMERAL等のsystem partition)全体を消去して
// maintenance modeへ戻すことを意味する。UserDisksToWipeを指定しないため、Longhorn、TopoLVM、raw volumeなど
// 別diskまたはuser diskとして構成されたdataはこの操作の対象外であり、消去されたと仮定してはならない。
func (c *Client) Reset(ctx context.Context) error {
	if c == nil || c.raw == nil {
		return ErrClientUnavailable
	}
	if err := c.raw.ResetGeneric(ctx, &machine.ResetRequest{
		Graceful:               true,
		Reboot:                 true,
		SystemPartitionsToWipe: nil,
		UserDisksToWipe:        nil,
		Mode:                   machine.ResetRequest_ALL,
	}); err != nil {
		return fmt.Errorf("reset talos node: %w", err)
	}
	return nil
}

// ShutdownはTalosの通常のshutdownを要求する。forceオプションは使わず、停止確認は呼び出し側が別途観測する。
func (c *Client) Shutdown(ctx context.Context) error {
	if c == nil || c.raw == nil {
		return ErrClientUnavailable
	}
	if err := c.raw.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown talos node: %w", err)
	}
	return nil
}

// dialは指定されたTLS configurationを使って単一のTalos endpointへのgRPC connectionを確立する。maintenance connectionはTLSで暗号化されるが自己署名証明書とclient certificateなしのため認証されない。DialMaintenanceとDialAuthenticatedを参照する。
func dial(ctx context.Context, endpoint string, tlsConfig *tls.Config) (*Client, error) {
	raw, err := talosclient.New(ctx,
		talosclient.WithTLSConfig(tlsConfig),
		talosclient.WithEndpoints(endpoint),
	)
	if err != nil {
		return nil, fmt.Errorf("dial talos endpoint %s: %w", endpoint, err)
	}
	return &Client{raw: raw}, nil
}
