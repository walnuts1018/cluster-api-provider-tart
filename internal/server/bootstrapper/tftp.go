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
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const maxTFTPFileSize int64 = 64 << 20

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

// resolveTFTPFilePath は、TFTP ルート相対の filename から安全なファイルパスを解決します。
// パストラバーサルの試みは拒否され、エラーを返します。
func resolveTFTPFilePath(root, filename string) (string, error) {
	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("failed to resolve tftp root: %w", err)
	}
	resolvedRoot, err = filepath.EvalSymlinks(resolvedRoot)
	if err != nil {
		return "", fmt.Errorf("failed to resolve tftp root: %w", err)
	}

	cleanedFilename := filepath.Clean(string(filepath.Separator) + filename)
	cleanedFilename = cleanedFilename[1:]
	filePath := filepath.Join(resolvedRoot, cleanedFilename)
	resolved, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve file path: %w", err)
	}
	if !hasPathPrefix(resolved, resolvedRoot) {
		return "", fmt.Errorf("access denied: path traversal detected")
	}
	return resolved, nil
}

// openTFTPFile は、解決済みパスのファイルを開きます。
// 通常ファイル以外（ディレクトリやデバイスファイルなど）は拒否されます。
func openTFTPFile(root, filename string, logger logr.Logger) (*os.File, error) {
	resolved, err := resolveTFTPFilePath(root, filename)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		if closeErr := file.Close(); closeErr != nil {
			logger.Error(closeErr, "Failed to close TFTP file after stat error", "filename", filename)
		}
		return nil, fmt.Errorf("failed to stat TFTP file: %w", err)
	}
	if info.Size() > maxTFTPFileSize {
		if closeErr := file.Close(); closeErr != nil {
			logger.Error(closeErr, "Failed to close oversized TFTP file", "filename", filename)
		}
		return nil, fmt.Errorf("access denied: file exceeds TFTP size limit")
	}
	if !info.Mode().IsRegular() {
		if closeErr := file.Close(); closeErr != nil {
			logger.Error(closeErr, "Failed to close non-regular TFTP file", "filename", filename)
		}
		return nil, fmt.Errorf("access denied: not a regular file")
	}
	return file, nil
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
// root は絶対パスとシンボリックリンク解決後のパスに事前解決されます。
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

	// ルートを絶対パスに解決し、シンボリックリンクを展開（一度だけ実行）
	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve tftp root path: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(resolvedRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate symlinks in tftp root: %w", err)
	}

	return &TFTPBootstrapper{
		root: realRoot,
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
		_, span := telemetry.Tracer.Start(ctx, "TFTP.ReadFile")
		defer span.End()

		span.SetAttributes(
			attribute.String("tftp.filename", filename),
		)

		lg.Info("TFTP read request", "filename", filename)
		file, err := openTFTPFile(b.root, filename, lg)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
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
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			lg.Error(err, "Failed to read TFTP file", "filename", filename)
			return err
		}

		span.SetStatus(codes.Ok, "")
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
		if err := conn.Close(); err != nil {
			lg.Error(err, "Failed to close TFTP UDP connection", "address", b.addr)
		}
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
		if err := b.Stop(); err != nil {
			lg.Error(err, "Failed to stop TFTP server after context cancellation")
		}
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
