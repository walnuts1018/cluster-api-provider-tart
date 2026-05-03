package bootstrapper

import "context"

// Bootstrapper は DHCP/TFTP ブートストラップサーバーのインターフェースです。
// ProxyDHCP として動作し、iPXE ローダを TFTP で配信します。
type Bootstrapper interface {
	// Start はブートストラップサーバーを起動します。
	Start(ctx context.Context) error
	// Addr はサーバーのアドレスを返します。
	Addr() string
	// NeedLeaderElection はリーダー選挙が必要かどうかを返します。
	NeedLeaderElection() bool
}
