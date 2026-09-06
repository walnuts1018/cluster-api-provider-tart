//go:build e2e

package lab

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Gatewayは、既存OSS wol-libvirt-gateway(https://github.com/brunoproduit/wol-libvirt-gateway)の
// プロセスを起動・停止するヘルパーである。このgatewayはUDP port 9でWake-on-LANマジックパケットを
// 受信し、libvirt domain XMLに定義されたMACアドレスから対応するdomainを自動解決してqemu:///system
// 経由で起動する。設定ファイルは不要で、バイナリを起動するだけで動作する。
//
// 既定のbind address(127.0.0.1:9)はloopbackのみで、lab bridge宛のbroadcastパケットが
// 届かないため、`--listen-address 0.0.0.0:9`を明示してすべてのinterfaceでlistenさせる
// (E2Eで実際にVMが起動しないことから発覚)。
type Gateway struct {
	cmd        *exec.Cmd
	stdout     bytes.Buffer
	stderr     bytes.Buffer
	mu         sync.Mutex
	binaryPath string
}

// StartGatewayは指定されたバイナリを起動し、常駐させる。
// libvirtUUというテスト用URIをそのまま渡す(通常は"qemu:///system")。
func StartGateway(ctx context.Context, binaryPath string, libvirtURI string) (*Gateway, error) {
	if binaryPath == "" {
		return nil, fmt.Errorf("wol-libvirt-gateway binary path is required")
	}
	if _, err := os.Stat(binaryPath); err != nil {
		return nil, fmt.Errorf("stat wol-libvirt-gateway binary %q: %w", binaryPath, err)
	}

	gw := &Gateway{binaryPath: binaryPath}
	// wol-libvirt-gatewayは設定ファイルを持たず、libvirt接続URIを環境変数またはデフォルトの
	// qemu:///systemから解決する実装のため、LIBVIRT_DEFAULT_URIで明示する。
	//nolint:gosec // binaryPathはCIのsetup-lab actionが配置した既知のパスであり、ユーザー入力を含まない
	cmd := exec.CommandContext(ctx, binaryPath, "--listen-address", "0.0.0.0:9")
	cmd.Env = append(os.Environ(), "LIBVIRT_DEFAULT_URI="+libvirtURI)
	cmd.Stdout = &gw.stdout
	cmd.Stderr = &gw.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start wol-libvirt-gateway: %w", err)
	}
	gw.cmd = cmd

	// 起動直後にプロセスが即座に終了していないかだけ簡易確認する(bind失敗などの早期検知)。
	select {
	case <-time.After(500 * time.Millisecond):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return nil, fmt.Errorf("wol-libvirt-gateway exited immediately: stdout=%q stderr=%q", gw.stdout.String(), gw.stderr.String())
	}

	return gw, nil
}

// Stopはgatewayプロセスをgracefulに終了させる。
func (g *Gateway) Stop() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cmd == nil || g.cmd.Process == nil {
		return nil
	}
	if err := g.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("stop wol-libvirt-gateway: %w", err)
	}
	// defer済みのerrorを握りつぶさずlogへ出す方針に従い、Waitのerrorも報告する。
	if err := g.cmd.Wait(); err != nil {
		return fmt.Errorf("wait wol-libvirt-gateway exit: %w", err)
	}
	return nil
}

// DumpLogsは収集済みのstdout/stderrを指定ディレクトリへ保存する(失敗時のdebug artifact用)。
func (g *Gateway) DumpLogs(artifactDir string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return fmt.Errorf("create artifact directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "wol-gateway-stdout.log"), g.stdout.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write wol-gateway stdout log: %w", err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "wol-gateway-stderr.log"), g.stderr.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write wol-gateway stderr log: %w", err)
	}
	return nil
}
