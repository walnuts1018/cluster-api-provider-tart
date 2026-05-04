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
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// iPXEBootFileNameAMD64 は amd64 用の iPXE ローダのファイル名です。
	iPXEBootFileNameAMD64 = "ipxe-x86_64.efi"
	// iPXEBootFileNameARM64 は arm64 用の iPXE ローダのファイル名です。
	iPXEBootFileNameARM64 = "ipxe-arm64.efi"
	// iPXEBootFileNameDefault はデフォルトの iPXE ローダのファイル名です。
	iPXEBootFileNameDefault = "ipxe.efi"
)

// Arch 型はクライアントのアーキテクチャを表します。
type Arch uint16

const (
	ArchIntelx86PC Arch = 0
	ArchEFIx8664   Arch = 7
	ArchEFIBC      Arch = 9
	ArchEFIARM64   Arch = 11
)

// DHCPBootstrapper は組み込み DHCP サーバーを用いた DHCP/TFTP ブートストラップサーバーの実装です。
// ProxyDHCP モードで動作し、既存のネットワークに影響を与えずに iPXE ローダを配信します。
type DHCPBootstrapper struct {
	tftpRoot string
	addr     string
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
		done:     make(chan struct{}),
		logger:   logr.Discard(),
	}, nil
}

// StartWithContext は DHCP サーバーを ProxyDHCP モードで起動します。
func (b *DHCPBootstrapper) StartWithContext(ctx context.Context) error {
	b.mu.Lock()
	lg := log.FromContext(ctx).WithName("bootstrapper")
	b.logger = lg
	b.mu.Unlock()

	// iPXE ローダの存在確認（オプション）
	for _, f := range []string{iPXEBootFileNameAMD64, iPXEBootFileNameARM64} {
		path := filepath.Join(b.tftpRoot, f)
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				lg.Info("iPXE bootloader is not found yet", "path", path)
			}
		}
	}

	// バインドアドレスからUDPアドレスを作成
	udpAddr, err := net.ResolveUDPAddr("udp4", b.addr)
	if err != nil {
		return fmt.Errorf("invalid bind address %s: %w", b.addr, err)
	}

	// ProxyDHCP は既存の DHCP サーバーより低い優先度で動作するため、
	// IPアドレスは割り当てず、ブートファイル名のみを提供します。
	handler := b.createDHCPHandler(ctx)

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
		if err := b.Stop(); err != nil {
			lg.Error(err, "Failed to stop DHCP server after context cancellation")
		}
	}()

	lg.Info("DHCP server started")
	return nil
}

// createDHCPHandler は DHCP パケットハンドラーを作成します。
func (b *DHCPBootstrapper) createDHCPHandler(ctx context.Context) server4.Handler {
	return func(conn net.PacketConn, peer net.Addr, m *dhcpv4.DHCPv4) {
		lg := b.logger.WithName("dhcp-handler")

		// BootRequestのみを処理
		if m.OpCode != dhcpv4.OpcodeBootRequest {
			return
		}

		_, span := telemetry.Tracer.Start(ctx, "DHCP.BootRequest")
		defer span.End()

		span.SetAttributes(
			attribute.String("dhcp.client_mac", m.ClientHWAddr.String()),
			attribute.String("dhcp.message_type", m.MessageType().String()),
			attribute.String("dhcp.transaction_id", fmt.Sprintf("%#x", m.TransactionID)),
		)

		// ProxyDHCP では、既存のDHCPサーバーが応答した後にのみ応答する
		// つまり、Option 54 (Server Identifier) が設定されていないパケットにのみ応答する
		serverID := m.GetOneOption(dhcpv4.OptionServerIdentifier)
		if serverID != nil {
			lg.Info("Skipping ProxyDHCP response, existing DHCP server already responded")
			span.SetAttributes(attribute.Bool("dhcp.proxy_skip", true))
			return
		}

		// クライアントのアーキテクチャを取得 (Option 93)
		arch := ArchEFIx8664 // Default
		if opt := m.GetOneOption(dhcpv4.OptionClientSystemArchitectureType); opt != nil {
			if len(opt) >= 2 {
				arch = Arch(uint16(opt[0])<<8 | uint16(opt[1]))
			}
		}

		bootFile := iPXEBootFileNameDefault
		switch arch {
		case ArchEFIx8664:
			bootFile = iPXEBootFileNameAMD64
		case ArchEFIARM64:
			bootFile = iPXEBootFileNameARM64
		default:
			lg.Info("Unknown architecture, using default boot file", "arch", arch)
		}

		// 新しいDHCPv4レスポンスを作成
		resp, err := dhcpv4.NewReplyFromRequest(m,
			dhcpv4.WithMessageType(dhcpv4.MessageTypeOffer),
			dhcpv4.WithOption(dhcpv4.OptBootFileName(bootFile)),
			dhcpv4.WithOption(dhcpv4.OptClassIdentifier("PXEClient")),
		)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			lg.Error(err, "Failed to create DHCP response")
			return
		}

		if _, err := conn.WriteTo(resp.ToBytes(), peer); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			lg.Error(err, "Failed to send DHCP response")
			return
		}

		span.SetStatus(codes.Ok, "")
		span.SetAttributes(
			attribute.String("dhcp.boot_file", bootFile),
			attribute.Int("dhcp.arch", int(arch)),
		)
		lg.Info("Sent DHCP Offer", "client_mac", m.ClientHWAddr.String(), "boot_file", bootFile, "arch", arch)
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
