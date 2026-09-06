package netboot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/server4"
)

const (
	// iPXEBootFileNameAMD64はamd64用のiPXEローダのファイル名である。
	iPXEBootFileNameAMD64 = "ipxe-x86_64.efi"
	// iPXEBootFileNameARM64はarm64用のiPXEローダのファイル名である。
	iPXEBootFileNameARM64 = "ipxe-arm64.efi"

	// dhcpPortはDHCPサーバーのポートである。
	dhcpPort = 67
	// pxePortはProxyDHCP(PXE)サーバーのポートである。
	pxePort = 4011
)

// Archはクライアントのアーキテクチャを表す。
type Arch uint16

const (
	ArchIntelx86PC Arch = 0
	ArchEFIx8664   Arch = 7
	ArchEFIBC      Arch = 9
	ArchEFIARM64   Arch = 11
)

// DHCPServerはProxyDHCPとして動作するDHCP/PXEサーバーである。
// 既存ネットワークのDHCPサーバーと共存し、IPアドレスの割り当ては行わずPXE bootに必要なオプションのみ応答する。
type DHCPServer struct {
	tftpRoot    string
	bindIP      string
	baseURL     string
	advertiseIP net.IP
	logger      *slog.Logger

	mu      sync.Mutex
	servers []*server4.Server
	done    chan struct{}
}

// NewDHCPServerは新しいDHCPServerを作成する。
// tftpRootはTFTPサーバーのルートディレクトリ、bindAddrはProxyDHCPのバインドアドレスである。
// advertiseAddrはクライアントに広告する到達可能なサーバーIP、baseURLはiPXEスクリプト配信用HTTPサーバーのベースURLである。
func NewDHCPServer(tftpRoot, bindAddr, advertiseAddr, baseURL string, logger *slog.Logger) (*DHCPServer, error) {
	if tftpRoot == "" {
		return nil, errors.New("tftpRoot is required")
	}
	if bindAddr == "" {
		return nil, errors.New("bindAddr is required")
	}
	if baseURL == "" {
		return nil, errors.New("baseURL is required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	bindIP, _, err := net.SplitHostPort(bindAddr)
	if err != nil {
		bindIP = bindAddr
	}

	advertiseIP := net.ParseIP(advertiseAddr)
	if advertiseIP == nil {
		return nil, fmt.Errorf("invalid advertise address: %s", advertiseAddr)
	}

	if err := os.MkdirAll(tftpRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create tftp root directory: %w", err)
	}

	return &DHCPServer{
		tftpRoot:    tftpRoot,
		bindIP:      bindIP,
		baseURL:     baseURL,
		advertiseIP: advertiseIP,
		logger:      logger.With("component", "dhcp"),
		done:        make(chan struct{}),
	}, nil
}

// Startはctxがキャンセルされるまでprocessをブロックし、ProxyDHCPサーバーを起動する。
func (s *DHCPServer) Start(ctx context.Context) error {
	lg := s.logger

	for _, f := range []string{iPXEBootFileNameAMD64, iPXEBootFileNameARM64} {
		path := filepath.Join(s.tftpRoot, f)
		if _, err := os.Stat(path); err != nil && errors.Is(err, os.ErrNotExist) {
			lg.Warn("ipxe bootloader is not found yet", "path", path)
		}
	}

	handler := s.createHandler()

	ports := []int{dhcpPort, pxePort}
	var servers []*server4.Server
	for _, port := range ports {
		addr := net.JoinHostPort(s.bindIP, strconv.Itoa(port))
		udpAddr, err := net.ResolveUDPAddr("udp4", addr)
		if err != nil {
			return fmt.Errorf("invalid bind address %s: %w", addr, err)
		}

		server, err := server4.NewServer("", udpAddr, handler)
		if err != nil {
			return fmt.Errorf("create DHCP server on port %d: %w", port, err)
		}
		servers = append(servers, server)
	}

	s.mu.Lock()
	s.servers = servers
	s.mu.Unlock()

	lg.Info("starting proxy DHCP servers", "bindIP", s.bindIP, "ports", ports)

	errCh := make(chan error, len(servers))
	var wg sync.WaitGroup
	for i, srv := range servers {
		wg.Add(1)
		go func(srv *server4.Server, port int) {
			defer wg.Done()
			if err := srv.Serve(); err != nil && !errors.Is(err, net.ErrClosed) {
				errCh <- fmt.Errorf("DHCP server on port %d exited: %w", port, err)
				return
			}
			errCh <- nil
		}(srv, ports[i])
	}

	go func() {
		<-ctx.Done()
		if err := s.Stop(); err != nil {
			lg.Error("failed to stop DHCP servers", "error", err)
		}
	}()

	wg.Wait()
	close(errCh)
	close(s.done)

	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

// Stopはサーバーを停止する。
func (s *DHCPServer) Stop() error {
	s.mu.Lock()
	servers := s.servers
	s.mu.Unlock()

	if len(servers) == 0 {
		return nil
	}

	s.logger.Info("stopping proxy DHCP servers")
	for _, server := range servers {
		if err := server.Close(); err != nil {
			s.logger.Error("error while stopping DHCP server", "error", err)
		}
	}
	return nil
}

func (s *DHCPServer) createHandler() server4.Handler {
	return func(conn net.PacketConn, peer net.Addr, m *dhcpv4.DHCPv4) {
		lg := s.logger

		localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
		if !ok {
			return
		}
		port := localAddr.Port

		if m.OpCode != dhcpv4.OpcodeBootRequest {
			return
		}

		lg.Debug("received DHCP packet", "port", port, "peer", peer, "messageType", m.MessageType(), "clientMAC", m.ClientHWAddr.String())

		// port 67(通常のDHCP)では、既存DHCPサーバーが既に応答したパケット(Server Identifierが設定済み)は無視する。
		if port == dhcpPort {
			if serverID := m.GetOneOption(dhcpv4.OptionServerIdentifier); serverID != nil {
				return
			}
		}

		// Option 93(Client System Architecture)がないrequestを勝手にamd64と推測すると、
		// 対象外のhostへブートローダを配信してしまうため、明示的にoptionの有無を確認する。
		arch, hasArchitecture := clientArchitecture(m)

		isIPXE := slices.Contains(m.UserClass(), "iPXE")
		bootFile, supported := agentBootFile(arch, hasArchitecture, isIPXE, s.baseURL, m.ClientHWAddr)
		if !supported {
			lg.Debug("ignoring unsupported PXE architecture", "arch", arch, "option93Present", hasArchitecture)
			return
		}

		var resp *dhcpv4.DHCPv4
		var err error

		if port == dhcpPort {
			// port 67: ProxyDHCP Offer。自身がProxyDHCPであることを名乗る(Option 60: PXEClient)。
			resp, err = dhcpv4.NewReplyFromRequest(m,
				dhcpv4.WithMessageType(dhcpv4.MessageTypeOffer),
				dhcpv4.WithOption(dhcpv4.OptServerIdentifier(s.advertiseIP)),
				dhcpv4.WithOption(dhcpv4.OptClassIdentifier("PXEClient")),
			)
		} else {
			// port 4011: PXE requestへの応答。実際のブートファイル名とTFTPサーバーIPを伝える。
			options := []dhcpv4.Modifier{
				dhcpv4.WithOption(dhcpv4.OptServerIdentifier(s.advertiseIP)),
				dhcpv4.WithOption(dhcpv4.OptClassIdentifier("PXEClient")),
			}
			if m.MessageType() == dhcpv4.MessageTypeRequest {
				options = append(options, dhcpv4.WithMessageType(dhcpv4.MessageTypeAck))
			}
			resp, err = dhcpv4.NewReplyFromRequest(m, options...)
			if err == nil {
				resp.BootFileName = bootFile
				resp.ServerIPAddr = s.advertiseIP
			}
		}

		if err != nil {
			lg.Error("failed to build DHCP response", "error", err)
			return
		}

		// ProxyDHCPではyiaddr(Your IP Address)は常に0.0.0.0であるべきである。
		resp.YourIPAddr = net.IPv4zero

		if _, err := conn.WriteTo(resp.ToBytes(), peer); err != nil {
			lg.Error("failed to send DHCP response", "error", err)
			return
		}

		lg.Info("sent DHCP response", "port", port, "clientMAC", m.ClientHWAddr.String(), "bootFile", bootFile, "arch", arch)
	}
}

func clientArchitecture(request *dhcpv4.DHCPv4) (Arch, bool) {
	option := request.GetOneOption(dhcpv4.OptionClientSystemArchitectureType)
	if len(option) < 2 {
		return 0, false
	}
	return Arch(uint16(option[0])<<8 | uint16(option[1])), true
}

func agentBootFile(arch Arch, optionPresent, isIPXE bool, baseURL string, mac net.HardwareAddr) (string, bool) {
	if !optionPresent || arch != ArchEFIx8664 {
		return "", false
	}
	if !isIPXE {
		return iPXEBootFileNameAMD64, true
	}
	macParam := url.QueryEscape(mac.String())
	return fmt.Sprintf("%s/ipxe?mac=%s", baseURL, macParam), true
}

// ResolveAdvertiseIPはクライアントへ広告するサーバーIPを解決する。
// advertiseAddrが明示的に設定されていればそれを使い、そうでなければbindAddr/httpAddrやnetwork interfaceから推測する。
func ResolveAdvertiseIP(bindAddr, httpAddr, advertiseAddr string) (net.IP, error) {
	if ip := net.ParseIP(advertiseAddr); ip != nil && !ip.IsUnspecified() {
		return ip, nil
	}

	for _, addr := range []string{bindAddr, httpAddr} {
		if ip := ParseHostIP(addr); ip != nil && !ip.IsUnspecified() {
			return ip, nil
		}
	}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("detect advertise address: %w", err)
	}
	var loopback net.IP
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP == nil {
			continue
		}
		ip := ipNet.IP.To4()
		if ip == nil || ip.IsUnspecified() {
			continue
		}
		if ip.IsLoopback() {
			if loopback == nil {
				loopback = ip
			}
			continue
		}
		return ip, nil
	}
	if loopback != nil {
		return loopback, nil
	}
	return nil, errors.New("failed to detect advertise address")
}

// DefaultAdvertiseHTTPBaseURLは、advertise-http-base-urlが明示設定されていない場合に、
// 広告用IPとHTTP bind addressのポートから既定のHTTP base URLを組み立てる。
// 単一NICのシンプルな構成であればこの自動検出で足りるため、site固有の設定なしでも
// clusterctl/cluster-api-operatorでのインストール直後にnetboot-serverが動作できる。
func DefaultAdvertiseHTTPBaseURL(dhcpBindAddress, httpBindAddress, advertiseAddress string) (string, error) {
	advertiseIP, err := ResolveAdvertiseIP(dhcpBindAddress, httpBindAddress, advertiseAddress)
	if err != nil {
		return "", err
	}
	_, port, err := net.SplitHostPort(httpBindAddress)
	if err != nil {
		return "", fmt.Errorf("invalid HTTP bind address %s: %w", httpBindAddress, err)
	}
	return fmt.Sprintf("http://%s:%s", advertiseIP.String(), port), nil
}

// ParseHostIPはhost[:port]形式またはhost単体の文字列からIPを取り出す。
func ParseHostIP(addr string) net.IP {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return net.ParseIP(host)
}
