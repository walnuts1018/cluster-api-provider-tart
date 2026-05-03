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

func openTFTPFile(root, filename string) (*os.File, error) {
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
		_ = file.Close()
		return nil, fmt.Errorf("failed to stat TFTP file: %w", err)
	}
	if info.Size() > maxTFTPFileSize {
		_ = file.Close()
		return nil, fmt.Errorf("access denied: file exceeds TFTP size limit")
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
		lg.Info("TFTP リードリクエスト", "filename", filename)
		file, err := openTFTPFile(b.root, filename)
		if err != nil {
			lg.Error(err, "ファイルパスの解決に失敗しました", "filename", filename)
			return err
		}
		defer func() {
			if err := file.Close(); err != nil {
				lg.Error(err, "TFTPファイルのクローズに失敗しました", "filename", filename)
			}
		}()

		_, err = rf.ReadFrom(file)
		if err != nil {
			lg.Error(err, "TFTPファイルの読み取りに失敗しました", "filename", filename)
			return err
		}
		return nil
	}

	server := tftp.NewServer(readHandler, nil)

	b.mu.Lock()
	b.server = server
	b.mu.Unlock()

	lg.Info("TFTP サーバーを起動します", "address", b.addr, "root", b.root)

	// サーバーの起動完了を待機するためのチャネル
	serveStarted := make(chan error, 1)
	// サーバーを別ゴルーチンで起動
	go func() {
		udpAddr, err := net.ResolveUDPAddr("udp4", b.addr)
		if err != nil {
			lg.Error(err, "UDPアドレスの解決に失敗しました", "address", b.addr)
			serveStarted <- fmt.Errorf("failed to resolve UDP address: %w", err)
			close(b.done)
			return
		}

		conn, err := net.ListenUDP("udp4", udpAddr)
		if err != nil {
			lg.Error(err, "UDPリスニングに失敗しました", "address", b.addr)
			serveStarted <- fmt.Errorf("failed to listen UDP: %w", err)
			close(b.done)
			return
		}

		close(serveStarted) // Serve()の呼び出し前にチャネルを閉じて開始をシグナル
		if err := server.Serve(conn); err != nil && !errors.Is(err, context.Canceled) {
			lg.Error(err, "TFTP サーバーがエラーで終了しました")
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

	lg.Info("TFTP サーバーを起動しました")
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
	lg.Info("TFTP サーバーを停止します")

	// サーバーを停止
	server.Shutdown()

	<-b.done
	lg.Info("TFTP サーバーを停止しました")
	return nil
}
