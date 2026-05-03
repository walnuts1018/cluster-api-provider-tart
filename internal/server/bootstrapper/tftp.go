package bootstrapper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/go-logr/logr"
	"github.com/pin/tftp/v3"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// hasPathPrefix は、target が prefix 以下のパスかどうかをチェックします。
func hasPathPrefix(path, prefix string) bool {
	rel, err := filepath.Rel(prefix, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if filepath.IsAbs(rel) {
		return false
	}
	return len(rel) > 0 && rel[0] != '.'
}

// TFTPBootstrapper は組み込み TFTP サーバーの実装です。
// iPXE ブートローダなどのファイルを配信します。
type TFTPBootstrapper struct {
	root   string
	addr   string
	server *tftp.Server
	logger logr.Logger
	mu     sync.Mutex
	done   chan struct{}
}

// NewTFTPBootstrapper は新しい TFTPBootstrapper を作成します。
// root は TFTP サーバーのルートディレクトリ、addr はバインドアドレスです。
func NewTFTPBootstrapper(root, addr string) (*TFTPBootstrapper, error) {
	if root == "" {
		return nil, fmt.Errorf("root is required")
	}
	if addr == "" {
		return nil, fmt.Errorf("addr is required")
	}

	// TFTP ルートディレクトリが存在することを確認
	if err := os.MkdirAll(root, 0755); err != nil {
		return nil, fmt.Errorf("failed to create tftp root directory: %w", err)
	}

	return &TFTPBootstrapper{
		root: root,
		addr: addr,
		done: make(chan struct{}),
	}, nil
}

// StartWithContext は TFTP サーバーを起動します。
func (b *TFTPBootstrapper) StartWithContext(ctx context.Context) error {
	lg := log.FromContext(ctx).WithName("tftp")
	b.mu.Lock()
	b.logger = lg
	b.mu.Unlock()

	// TFTP サーバーを作成
	readHandler := func(filename string, rf io.ReaderFrom) error {
		lg.Info("TFTP read request", "filename", filename)
		filePath := filepath.Join(b.root, filename)
		resolved, err := filepath.Abs(filePath)
		if err != nil {
			lg.Error(err, "Failed to resolve file path", "filename", filename)
			return fmt.Errorf("failed to resolve file path: %w", err)
		}
		if !hasPathPrefix(resolved, b.root) {
			lg.Error(nil, "Path traversal attempt detected", "filename", filename, "requested_path", resolved)
			return fmt.Errorf("access denied: path traversal detected")
		}
		file, err := os.Open(resolved)
		if err != nil {
			lg.Error(err, "Failed to open TFTP file", "filename", filename)
			return err
		}
		defer func() {
			if err := file.Close(); err != nil {
				lg.Error(err, "Failed to close TFTP file", "filename", filename)
			}
		}()

		_, err = rf.ReadFrom(file)
		if err != nil {
			lg.Error(err, "Failed to read TFTP file", "filename", filename)
			return err
		}
		return nil
	}

	server := tftp.NewServer(readHandler, nil)

	b.mu.Lock()
	b.server = server
	b.mu.Unlock()

	lg.Info("Starting TFTP server", "address", b.addr, "root", b.root)

	// サーバーの起動完了を待機するためのチャネル
	serveStarted := make(chan error, 1)
	// サーバーを別ゴルーチンで起動
	go func() {
		udpAddr, err := net.ResolveUDPAddr("udp4", b.addr)
		if err != nil {
			lg.Error(err, "Failed to resolve UDP address", "address", b.addr)
			serveStarted <- fmt.Errorf("failed to resolve UDP address: %w", err)
			close(b.done)
			return
		}

		conn, err := net.ListenUDP("udp4", udpAddr)
		if err != nil {
			lg.Error(err, "Failed to listen UDP", "address", b.addr)
			serveStarted <- fmt.Errorf("failed to listen UDP: %w", err)
			close(b.done)
			return
		}

		close(serveStarted) // Serve()の呼び出し前にチャネルを閉じて開始をシグナル
		if err := server.Serve(conn); err != nil && !errors.Is(err, context.Canceled) {
			lg.Error(err, "TFTP server exited with error")
		}
		_ = conn.Close()
		close(b.done)
	}()

	// サーバーの起動完了を待機（起動エラーがあれば即座に返す）
	select {
	case err := <-serveStarted:
		if err != nil {
			return fmt.Errorf("TFTP server failed to start: %w", err)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	// コンテキストがキャンセルされた場合はサーバーも停止する
	go func() {
		<-ctx.Done()
		_ = b.Stop()
	}()

	lg.Info("TFTP server started")
	return nil
}

// Addr はサーバーのアドレスを返します。
func (b *TFTPBootstrapper) Addr() string {
	return b.addr
}

// NeedLeaderElection はリーダー選挙が必要ないことを返します。
func (b *TFTPBootstrapper) NeedLeaderElection() bool {
	return false
}

// Stop はTFTPサーバーを停止します。
func (b *TFTPBootstrapper) Stop() error {
	b.mu.Lock()
	server := b.server
	b.mu.Unlock()

	if server == nil {
		return nil
	}

	lg := b.logger.WithName("tftp")
	lg.Info("Stopping TFTP server")

	// サーバーを停止
	server.Shutdown()

	<-b.done
	lg.Info("TFTP server stopped")
	return nil
}
