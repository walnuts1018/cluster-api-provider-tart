package bootstrapper

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/go-logr/logr"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/server4"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// iPXEBootFileName は iPXE ローダのファイル名です。
	iPXEBootFileName = "undionly.kpxe"
)

// DHCPBootstrapper は組み込み DHCP サーバーを用いた DHCP/TFTP ブートストラップサーバーの実装です。
// ProxyDHCP モードで動作し、既存のネットワークに影響を与えずに iPXE ローダを配信します。
type DHCPBootstrapper struct {
	tftpRoot string
	addr     string
	iPXEPath string
	server   *server4.Server
	logger   logr.Logger
	mu       sync.Mutex
	done     chan struct{}
}

// NewDHCPBootstrapper は新しい DHCPBootstrapper を作成します。
// tftpRoot は TFTP サーバーのルートディレクトリ、addr は ProxyDHCP のバインドアドレスです。
func NewDHCPBootstrapper(tftpRoot, addr string) (*DHCPBootstrapper, error) {
	if tftpRoot == "" {
		return nil, fmt.Errorf("tftpRoot is required")
	}
	if addr == "" {
		return nil, fmt.Errorf("addr is required")
	}

	// TFTP ルートディレクトリが存在することを確認
	if err := os.MkdirAll(tftpRoot, 0755); err != nil {
		return nil, fmt.Errorf("failed to create tftp root directory: %w", err)
	}

	return &DHCPBootstrapper{
		tftpRoot: tftpRoot,
		addr:     addr,
		iPXEPath: filepath.Join(tftpRoot, iPXEBootFileName),
		done:     make(chan struct{}),
		logger:   logr.Discard(),
	}, nil
}

// StartWithContext は DHCP サーバーを ProxyDHCP モードで起動します。
func (b *DHCPBootstrapper) StartWithContext(ctx context.Context) error {
	lg := log.FromContext(ctx).WithName("bootstrapper")
	b.mu.Lock()
	b.logger = lg
	b.mu.Unlock()

	// iPXE ローダが存在することを確認
	if _, err := os.Stat(b.iPXEPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("iPXE bootloader not found at %s. Please place undionly.kpxe or ipxe.kpxe in the TFTP root directory", b.iPXEPath)
		}
		return fmt.Errorf("failed to check iPXE bootloader: %w", err)
	}

	// バインドアドレスからUDPアドレスを作成
	udpAddr, err := net.ResolveUDPAddr("udp4", b.addr)
	if err != nil {
		return fmt.Errorf("invalid bind address %s: %w", b.addr, err)
	}

	// ProxyDHCP は既存の DHCP サーバーより低い優先度で動作するため、
	// IPアドレスは割り当てず、ブートファイル名のみを提供します。
	handler := b.createDHCPHandler()

	server, err := server4.NewServer("", udpAddr, handler)
	if err != nil {
		return fmt.Errorf("failed to create DHCP server: %w", err)
	}

	b.mu.Lock()
	b.server = server
	b.mu.Unlock()

	lg.Info("Starting DHCP server", "address", b.addr)

	// サーバーの起動完了を待機するためのチャネル
	serveStarted := make(chan struct{})
	// サーバーを別ゴルーチンで起動
	go func() {
		close(serveStarted) // Serve()の呼び出し前にチャネルを閉じて開始をシグナル
		if err := server.Serve(); err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, net.ErrClosed) {
				lg.Error(err, "DHCP server exited with error")
			}
		}
		close(b.done)
	}()

	// サーバーの起動完了を待機（Serveが開始されたことを確認）
	select {
	case <-serveStarted:
		// Serve()が正常に開始された
	case <-ctx.Done():
		return ctx.Err()
	}

	// コンテキストがキャンセルされた場合はサーバーも停止する
	go func() {
		<-ctx.Done()
		_ = b.Stop()
	}()

	lg.Info("DHCP サーバーを起動しました")
	return nil
}

// createDHCPHandler は DHCP パケットハンドラーを作成します。
func (b *DHCPBootstrapper) createDHCPHandler() server4.Handler {
	return func(conn net.PacketConn, peer net.Addr, m *dhcpv4.DHCPv4) {
		lg := b.logger.WithName("dhcp-handler")

		// BootRequestのみを処理
		if m.OpCode != dhcpv4.OpcodeBootRequest {
			return
		}

		// ProxyDHCP では、既存のDHCPサーバーが応答した後にのみ応答する
		// つまり、Option 54 (Server Identifier) が設定されていないパケットにのみ応答する
		serverID := m.GetOneOption(dhcpv4.OptionServerIdentifier)
		if serverID != nil {
			lg.Info("Skipping ProxyDHCP response, existing DHCP server already responded")
			return
		}

		// 新しいDHCPv4レスポンスを作成
		resp, err := dhcpv4.NewReplyFromRequest(m,
			dhcpv4.WithMessageType(dhcpv4.MessageTypeOffer),
			dhcpv4.WithOption(dhcpv4.OptBootFileName(iPXEBootFileName)),
			dhcpv4.WithOption(dhcpv4.OptClassIdentifier("PXEClient")),
		)
		if err != nil {
			lg.Error(err, "Failed to create DHCP response")
			return
		}

		if _, err := conn.WriteTo(resp.ToBytes(), peer); err != nil {
			lg.Error(err, "Failed to send DHCP response")
			return
		}

		lg.Info("Sent DHCP Offer", "client_mac", m.ClientHWAddr.String(), "boot_file", iPXEBootFileName)
	}
}

// Addr はサーバーのアドレスを返します。
func (b *DHCPBootstrapper) Addr() string {
	return b.addr
}

// NeedLeaderElection はリーダー選挙が必要ないことを返します。
func (b *DHCPBootstrapper) NeedLeaderElection() bool {
	return false
}

// Stop はDHCPサーバーを停止します。
func (b *DHCPBootstrapper) Stop() error {
	b.mu.Lock()
	server := b.server
	b.mu.Unlock()

	if server == nil {
		return nil
	}

	lg := b.logger.WithName("bootstrapper")
	lg.Info("Stopping DHCP server")

	if err := server.Close(); err != nil {
		lg.Error(err, "Error occurred while stopping DHCP server")
		return fmt.Errorf("failed to close DHCP server: %w", err)
	}

	lg.Info("DHCP server stopped")
	return nil
}
