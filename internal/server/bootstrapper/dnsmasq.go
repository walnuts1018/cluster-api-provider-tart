package bootstrapper

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// defaultTFTPPort は TFTP のデフォルトポートです。
	defaultTFTPPort = 69
	// iPXEBootFileName は iPXE ローダのファイル名です。
	iPXEBootFileName = "undionly.kpxe"
	// dnsmasqBinary は dnsmasq のバイナリ名です。
	dnsmasqBinary = "dnsmasq"
)

// DnsmasqBootstrapper は dnsmasq を用いた DHCP/TFTP ブートストラップサーバーの実装です。
// ProxyDHCP モードで動作し、既存のネットワークに影響を与えずに iPXE ローダを配信します。
type DnsmasqBootstrapper struct {
	tftpRoot   string
	addr       string
	iPXEPath   string
	dnsmasqCmd *exec.Cmd
	dnsmasqPID int
}

// NewDnsmasqBootstrapper は新しい DnsmasqBootstrapper を作成します。
// tftpRoot は TFTP サーバーのルートディレクトリ、addr は ProxyDHCP のバインドアドレスです。
func NewDnsmasqBootstrapper(tftpRoot, addr string) (*DnsmasqBootstrapper, error) {
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

	return &DnsmasqBootstrapper{
		tftpRoot: tftpRoot,
		addr:     addr,
		iPXEPath: filepath.Join(tftpRoot, iPXEBootFileName),
	}, nil
}

// Start は dnsmasq を ProxyDHCP モードで起動します。
func (b *DnsmasqBootstrapper) Start(ctx context.Context) error {
	lg := log.FromContext(ctx).WithName("bootstrapper")

	// iPXE ローダが存在することを確認
	if _, err := os.Stat(b.iPXEPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("iPXE bootloader not found at %s. Please place undionly.kpxe or ipxe.kpxe in the TFTP root directory", b.iPXEPath)
		}
		return fmt.Errorf("failed to check iPXE bootloader: %w", err)
	}

	// バインドアドレスからポートを抽出
	_, portStr, err := net.SplitHostPort(b.addr)
	if err != nil {
		return fmt.Errorf("invalid bind address %s: %w", b.addr, err)
	}

	// dnsmasq の引数を構築
	args := []string{
		"--dhcp-range=12.0.0.0,255.255.255.252,proxy",
		"--dhcp-port=" + portStr,
		"--enable-tftp",
		"--tftp-root=" + b.tftpRoot,
		"--tftp-port=" + fmt.Sprintf("%d", defaultTFTPPort),
		"--dhcp-boot=" + iPXEBootFileName,
		"--log-queries",
		"--log-dhcp",
		"--no-daemon",
		"--interface=" + extractInterface(b.addr),
	}

	// dnsmasq プロセスを起動
	cmd := exec.CommandContext(ctx, dnsmasqBinary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	lg.Info("dnsmasq を起動します", "args", args)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start dnsmasq: %w", err)
	}

	b.dnsmasqCmd = cmd
	b.dnsmasqPID = cmd.Process.Pid

	lg.Info("dnsmasq を起動しました", "pid", b.dnsmasqPID)

	// コンテキストがキャンセルされるまで dnsmasq プロセスの終了を待機
	// コンテキストがキャンセルされた場合はプロセスも終了する
	go func() {
		<-ctx.Done()
		_ = b.Stop()
	}()

	// dnsmasq プロセスの終了を待機
	err = cmd.Wait()
	if err != nil && !strings.Contains(err.Error(), "signal:") {
		// シグナルによる終了以外は無視
		lg.Error(err, "dnsmasq プロセスがエラーで終了しました")
	}

	return nil
}

// Addr はサーバーのアドレスを返します。
func (b *DnsmasqBootstrapper) Addr() string {
	return b.addr
}

// NeedLeaderElection はリーダー選挙が必要ないことを返します。
func (b *DnsmasqBootstrapper) NeedLeaderElection() bool {
	return false
}

// extractInterface はバインドアドレスからインターフェース名を抽出します。
// 例: "eth0:67" -> "eth0", "0.0.0.0:67" -> "" (all interfaces)
func extractInterface(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// ポートなしの場合は addr 全体をホストとして扱う
		host = addr
	}

	// ホスト名にコロンが含まれている場合はインターフェースとして扱う
	if idx := indexOf(host, ':'); idx != -1 {
		return host[:idx]
	}

	// 0.0.0.0 や :: はすべてのインターフェースを意味する
	if host == "0.0.0.0" || host == "::" || host == "" {
		return ""
	}

	// ホスト名が空でない場合はそれをインターフェースとして扱う（例: eth0:67 の場合）
	if host != "" {
		return host
	}

	// デフォルトは空（すべてのインターフェース）
	return ""
}

// indexOf は文字列内で指定された文字の最初のインデックスを返します。
func indexOf(s string, c byte) int {
	for i := range len(s) {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// Stop は dnsmasq プロセスを停止します。
func (b *DnsmasqBootstrapper) Stop() error {
	if b.dnsmasqCmd == nil || b.dnsmasqCmd.Process == nil {
		return nil
	}

	lg := log.FromContext(context.Background()).WithName("bootstrapper")
	lg.Info("dnsmasq を停止します", "pid", b.dnsmasqPID)

	// プロセスにシグナルを送って graceful shutdown を試みる
	if err := b.dnsmasqCmd.Process.Signal(os.Interrupt); err != nil {
		return fmt.Errorf("failed to send interrupt signal: %w", err)
	}

	// 5秒以内に終了するか確認
	done := make(chan error, 1)
	go func() {
		done <- b.dnsmasqCmd.Wait()
	}()

	select {
	case <-time.After(5 * time.Second):
		// タイムアウトしたら強制終了
		lg.Info("dnsmasq が 5 秒以内に終了しなかったため、強制終了します", "pid", b.dnsmasqPID)
		if err := b.dnsmasqCmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill dnsmasq process: %w", err)
		}
	case err := <-done:
		if err != nil {
			return fmt.Errorf("dnsmasq process exited with error: %w", err)
		}
	}

	lg.Info("dnsmasq を停止しました", "pid", b.dnsmasqPID)
	return nil
}
