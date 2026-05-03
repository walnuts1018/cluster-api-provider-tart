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
		filePath := filepath.Join(b.root, filename)
		file, err := os.Open(filePath)
		if err != nil {
			lg.Error(err, "TFTPファイルの開閉に失敗しました", "filename", filename)
			return err
		}
		defer file.Close()

		_, err = rf.ReadFrom(file)
		if err != nil {
			lg.Error(err, "TFTPファイルの読み取りに失敗しました", "filename", filename)
			return err
		}
		return nil
	}

	writeHandler := func(filename string, wt io.WriterTo) error {
		lg.Info("TFTP ライトリクエスト", "filename", filename)
		filePath := filepath.Join(b.root, filename)
		file, err := os.Create(filePath)
		if err != nil {
			lg.Error(err, "TFTPファイルの作成に失敗しました", "filename", filename)
			return err
		}
		defer file.Close()

		_, err = wt.WriteTo(file)
		if err != nil {
			lg.Error(err, "TFTPファイルの書き込みに失敗しました", "filename", filename)
			return err
		}
		return nil
	}

	server := tftp.NewServer(readHandler, writeHandler)

	b.mu.Lock()
	b.server = server
	b.mu.Unlock()

	lg.Info("TFTP サーバーを起動します", "address", b.addr, "root", b.root)

	// サーバーを別ゴルーチンで起動
	go func() {
		udpAddr, err := net.ResolveUDPAddr("udp4", b.addr)
		if err != nil {
			lg.Error(err, "UDPアドレスの解決に失敗しました", "address", b.addr)
			close(b.done)
			return
		}

		conn, err := net.ListenUDP("udp4", udpAddr)
		if err != nil {
			lg.Error(err, "UDPリスニングに失敗しました", "address", b.addr)
			close(b.done)
			return
		}

		if err := server.Serve(conn); err != nil && !errors.Is(err, context.Canceled) {
			lg.Error(err, "TFTP サーバーがエラーで終了しました")
		}
		conn.Close()
		close(b.done)
	}()

	// コンテキストがキャンセルされた場合はサーバーも停止する
	go func() {
		<-ctx.Done()
		b.Stop()
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
