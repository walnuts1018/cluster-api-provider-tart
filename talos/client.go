// Package talos adapts the Talos machinery gRPC client to the small set of
// observations and operations Tart needs, so that generated Talos API types never
// leak into controller or policy packages. See .agents/skills/talos/SKILL.md.
package talos

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"uuid"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/container"
	k8sconfig "github.com/siderolabs/talos/pkg/machinery/config/types/k8s"
	configmeta "github.com/siderolabs/talos/pkg/machinery/config/types/meta"
	runtimeconfig "github.com/siderolabs/talos/pkg/machinery/config/types/runtime"
	v1alpha1config "github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
	machineryhardware "github.com/siderolabs/talos/pkg/machinery/resources/hardware"
	machinerynetwork "github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"

	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

// ErrClientUnavailableは接続済みのclientなしでTalos operationが要求されたことを示す。
var (
	ErrClientUnavailable  = errors.New("talos client is unavailable")
	ErrProviderIDConflict = errors.New("talos kubelet provider ID conflicts with the allocated Host")
)

// InstallerImage returns the Image Factory installer reference for a desired
// Talos image identity.
func InstallerImage(version, schematicID string) (string, error) {
	version = strings.TrimSpace(version)
	schematicID = strings.TrimSpace(schematicID)
	if version == "" {
		return "", errors.New("talos image version is empty")
	}
	if schematicID == "" {
		return "", errors.New("talos image schematic ID is empty")
	}

	return fmt.Sprintf("factory.talos.dev/metal-installer/%s:%s", schematicID, version), nil
}

// SetInstallerImage updates only the Talos installer image in a complete
// machine configuration. Talos machinery owns the document merge and
// serialization so existing disk, PKI, and machine settings remain intact.
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
		// TODO: 旧Talos設定形式はサポートしなくていいのでは？最新のtalosで動作すればいいです。
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

// SetProviderID writes the provider ID derived from the allocated TartHost into
// the kubelet configuration. The value is checked before applying the patch so
// a user-owned conflicting provider ID cannot silently be replaced.
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

// Client is a thin wrapper around the Talos machinery gRPC client. It exposes only the
// observations and operations Tart's reconcile and policy packages need.
type Client struct {
	raw *talosclient.Client
}

// Close releases the underlying gRPC connection.
func (c *Client) Close() error {
	if c == nil || c.raw == nil {
		return nil
	}
	return c.raw.Close()
}

// Version is the observed Talos OS version and platform reported by the machine's
// authenticated or maintenance API.
type Version struct {
	Tag      string
	SHA      string
	Platform string
	Arch     string
}

// Version fetches the observed Talos OS version from the connected node.
//
// TODO: 深い安全ロジック(schematic比較、health判定、reboot後の再接続判断)は次セッションで
// host/controlplane側のpolicyへ実装する。ここではTalos APIから値を取得するだけに留める。
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

// ApplyConfigurationはcomplete Talos machine configurationをmaintenance APIへ渡す。TalosはconfigurationのUnattendedInstallConfigまたはnative設定に従ってinstallationとrebootを実行する。
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

// Bootstrap starts the Talos control-plane etcd bootstrap operation. The caller
// must invoke it only after the authenticated control-plane machine is reachable.
func (c *Client) Bootstrap(ctx context.Context) error {
	if c == nil || c.raw == nil {
		return ErrClientUnavailable
	}
	if err := c.raw.Bootstrap(ctx, &machine.BootstrapRequest{}); err != nil {
		return fmt.Errorf("bootstrap talos control plane: %w", err)
	}
	return nil
}

// EtcdStatus is the small etcd observation needed to distinguish a running
// control-plane member from a machine that has only finished OS installation.
type EtcdStatus struct {
	MemberID uint64
	Leader   uint64
	Errors   []string
}

// EtcdStatus observes the local etcd member through the authenticated Talos API.
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

// Kubeconfig returns the workload-cluster kubeconfig from the authenticated
// Talos API. The caller must keep the bytes in memory and must not expose them
// through status, events, logs, or metrics.
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

// Inventory contains the stable hardware identity observed through the Talos
// maintenance API. It deliberately hides Talos resource types from callers.
type Inventory struct {
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

// HasMAC reports whether the observed physical links contain the expected Host
// enrollment identity.
func (i Inventory) HasMAC(expected network.MACAddress) bool {
	if expected.IsZero() {
		return false
	}
	return slices.Contains(i.MACAddresses, expected)
}

// Inventory reads the hardware identity available before authentication. The MAC
// address is used to bind a configured endpoint to the claimed TartHost.
func (c *Client) Inventory(ctx context.Context) (Inventory, error) {
	if c == nil || c.raw == nil {
		return Inventory{}, ErrClientUnavailable
	}
	links, err := safe.ReaderListAll[*machinerynetwork.LinkStatus](ctx, c.raw.COSI)
	if err != nil {
		return Inventory{}, fmt.Errorf("list talos network links: %w", err)
	}

	observed := Inventory{}
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

func parseHardwareAddress(value []byte) (network.MACAddress, error) {
	if len(value) == 0 {
		return network.MACAddress(""), nil
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

// dial establishes a gRPC connection to a single Talos endpoint using the given TLS
// configuration. Maintenance connections are TLS-encrypted but not authenticated
// (self-signed, no client certificate); see DialMaintenance. Authenticated connections
// present a client certificate; see DialAuthenticated.
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
