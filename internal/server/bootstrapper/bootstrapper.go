package bootstrapper

import (
	"context"
	"fmt"
)

// Bootstrapper は DHCP/TFTP ブートストラップサーバーのインターフェースです。
// ProxyDHCP として動作し、iPXE ローダを TFTP で配信します。
type Bootstrapper interface {
	// StartWithContext はブートストラップサーバーを起動します。
	StartWithContext(ctx context.Context) error
	// Addr はサーバーのアドレスを返します。
	Addr() string
	// NeedLeaderElection はリーダー選挙が必要かどうかを返します。
	NeedLeaderElection() bool
}

// CombinedBootstrapper は DHCP と TFTP サーバーを統合したブートストラップサーバーのインターフェースです。
// manager.Runnable として動作し、コントローラーマネージャーに統合されます。
type CombinedBootstrapper interface {
	Bootstrapper
	// Start はサーバーを起動します（manager.Runnable準拠）。
	Start(context.Context) error
	// Stop はサーバーを停止します。
	Stop() error
	// DHCPBootFileName は iPXE ブートローダのファイル名を返します。
	DHCPBootFileName() string
	// TFTPRoot は TFTP サーバーのルートディレクトリを返します。
	TFTPRoot() string
}

// combinedBootstrapperImpl は CombinedBootstrapper の実装です。
type combinedBootstrapperImpl struct {
	dhcp *DHCPBootstrapper
	tftp *TFTPBootstrapper
}

// NewCombinedBootstrapper は新しい CombinedBootstrapper を作成します。
func NewCombinedBootstrapper(tftpRoot, bootstrapBindAddress string) (CombinedBootstrapper, error) {
	if tftpRoot == "" {
		return nil, fmt.Errorf("tftpRoot is required")
	}
	if bootstrapBindAddress == "" {
		return nil, fmt.Errorf("bootstrapBindAddress is required")
	}

	dhcp, err := NewDHCPBootstrapper(tftpRoot, bootstrapBindAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to create DHCP bootstrapper: %w", err)
	}

	tftp, err := NewTFTPBootstrapper(tftpRoot, ":69")
	if err != nil {
		return nil, fmt.Errorf("failed to create TFTP bootstrapper: %w", err)
	}

	return &combinedBootstrapperImpl{
		dhcp: dhcp,
		tftp: tftp,
	}, nil
}

// Start は DHCP と TFTP サーバーを起動します（manager.Runnable準拠）。
func (c *combinedBootstrapperImpl) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// TFTP サーバーを先に起動
	if err := c.tftp.StartWithContext(ctx); err != nil {
		return fmt.Errorf("failed to start TFTP server: %w", err)
	}

	// DHCP サーバーを起動
	if err := c.dhcp.StartWithContext(ctx); err != nil {
		return fmt.Errorf("failed to start DHCP server: %w", err)
	}

	// コンテキストがキャンセルされるまで待機
	<-ctx.Done()
	return nil
}

// Stop は DHCP と TFTP サーバーを停止します。
func (c *combinedBootstrapperImpl) Stop() error {
	if err := c.dhcp.Stop(); err != nil {
		return fmt.Errorf("failed to stop DHCP server: %w", err)
	}
	if err := c.tftp.Stop(); err != nil {
		return fmt.Errorf("failed to stop TFTP server: %w", err)
	}
	return nil
}

// Addr は DHCP サーバーのアドレスを返します。
func (c *combinedBootstrapperImpl) Addr() string {
	return c.dhcp.Addr()
}

// NeedLeaderElection はリーダー選挙が必要ないことを返します。
func (c *combinedBootstrapperImpl) NeedLeaderElection() bool {
	return false
}

// DHCPBootFileName は iPXE ブートローダのファイル名を返します。
func (c *combinedBootstrapperImpl) DHCPBootFileName() string {
	return iPXEBootFileName
}

// TFTPRoot は TFTP サーバーのルートディレクトリを返します。
func (c *combinedBootstrapperImpl) TFTPRoot() string {
	return c.tftp.root
}

// Start は Bootstrapperインターフェースの実装です（内部用）。
func (c *combinedBootstrapperImpl) StartWithContext(ctx context.Context) error {
	return c.Start(nil)
}
