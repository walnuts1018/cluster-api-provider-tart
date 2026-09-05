// Package talos adapts the Talos machinery gRPC client to the small set of
// observations and operations Tart needs, so that generated Talos API types never
// leak into controller or policy packages. See .agents/skills/talos/SKILL.md.
package talos

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	machineryhardware "github.com/siderolabs/talos/pkg/machinery/resources/hardware"
	machinerynetwork "github.com/siderolabs/talos/pkg/machinery/resources/network"

	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

// ErrClientUnavailableは接続済みのclientなしでTalos operationが要求されたことを示す。
var ErrClientUnavailable = errors.New("talos client is unavailable")

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
	}, nil
}

// ApplyConfiguration submits a complete Talos machine configuration through the
// maintenance API. Talos performs the installation and reboot according to the
// machine.install section; Tart does not write disks or reimplement that lifecycle.
func (c *Client) ApplyConfiguration(ctx context.Context, configuration []byte) error {
	if c == nil || c.raw == nil {
		return ErrClientUnavailable
	}
	if len(configuration) == 0 {
		return errors.New("talos machine configuration is empty")
	}
	if _, err := c.raw.ApplyConfiguration(ctx, &machine.ApplyConfigurationRequest{Data: configuration}); err != nil {
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
	SystemUUID   string
	MACAddresses []string
}

// HasMAC reports whether the observed physical links contain the expected Host
// enrollment identity.
func (i Inventory) HasMAC(expected string) bool {
	want, err := net.ParseMAC(strings.TrimSpace(expected))
	if err != nil {
		return false
	}
	for _, observed := range i.MACAddresses {
		got, err := net.ParseMAC(strings.TrimSpace(observed))
		if err == nil && got.String() == want.String() {
			return true
		}
	}
	return false
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
	for link := range links.All() {
		if !link.TypedSpec().Physical() {
			continue
		}
		for _, address := range []net.HardwareAddr{
			net.HardwareAddr(link.TypedSpec().HardwareAddr),
			net.HardwareAddr(link.TypedSpec().PermanentAddr),
		} {
			if len(address) != 0 {
				observed.MACAddresses = append(observed.MACAddresses, address.String())
			}
		}
	}
	if len(observed.MACAddresses) == 0 {
		return Inventory{}, errors.New("talos maintenance inventory has no physical MAC address")
	}

	systems, err := safe.ReaderListAll[*machineryhardware.SystemInformation](ctx, c.raw.COSI)
	if err == nil {
		for system := range systems.All() {
			if uuid := strings.TrimSpace(system.TypedSpec().UUID); uuid != "" {
				observed.SystemUUID = uuid
				break
			}
		}
	}

	return observed, nil
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
