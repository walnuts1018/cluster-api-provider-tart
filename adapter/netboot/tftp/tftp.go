// Package tftpは、netboot-serverが提供するTFTPサーバーの実装である。
// iPXEブートローダなどの初期ブートファイルのみを配信し、kernel/initramfsの取得は
// iPXEスクリプトを経由したHTTPで行うため、ここでは小さいファイルのみを扱う。
package tftp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/pin/tftp/v3"
)

// maxFileSizeはTFTP経由で配信するファイルの上限サイズである。
const maxFileSize int64 = 64 << 20

// hasPathPrefixはtargetがprefix以下のパスかどうかを判定する。
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

// resolveFilePathは、TFTPルート相対のfilenameから安全なファイルパスを解決する。
// パストラバーサルの試みは拒否しerrorを返す。
func resolveFilePath(root, filename string) (string, error) {
	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve tftp root: %w", err)
	}
	resolvedRoot, err = filepath.EvalSymlinks(resolvedRoot)
	if err != nil {
		return "", fmt.Errorf("resolve tftp root: %w", err)
	}

	cleanedFilename := filepath.Clean(string(filepath.Separator) + filename)
	cleanedFilename = cleanedFilename[1:]
	filePath := filepath.Join(resolvedRoot, cleanedFilename)
	resolved, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		return "", fmt.Errorf("resolve file path: %w", err)
	}
	if !hasPathPrefix(resolved, resolvedRoot) {
		return "", errors.New("access denied: path traversal detected")
	}
	return resolved, nil
}

// openFileは、解決済みパスのファイルを開く。通常ファイル以外(ディレクトリ、デバイスファイルなど)は拒否する。
func openFile(root, filename string, logger *slog.Logger) (*os.File, error) {
	resolved, err := resolveFilePath(root, filename)
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
			logger.Error("failed to close TFTP file after stat error", "filename", filename, "error", closeErr)
		}
		return nil, fmt.Errorf("stat TFTP file: %w", err)
	}
	if info.Size() > maxFileSize {
		if closeErr := file.Close(); closeErr != nil {
			logger.Error("failed to close oversized TFTP file", "filename", filename, "error", closeErr)
		}
		return nil, errors.New("access denied: file exceeds TFTP size limit")
	}
	if !info.Mode().IsRegular() {
		if closeErr := file.Close(); closeErr != nil {
			logger.Error("failed to close non-regular TFTP file", "filename", filename, "error", closeErr)
		}
		return nil, errors.New("access denied: not a regular file")
	}
	return file, nil
}

// Serverは、iPXEブートローダなどの初期ブートファイルのみを配信するTFTPサーバーである。
type Server struct {
	root   string
	addr   string
	logger *slog.Logger

	mu         sync.Mutex
	server     *tftp.Server
	actualAddr string
	done       chan struct{}
}

// NewServerは新しいServerを作成する。
// rootはTFTPサーバーのルートディレクトリ、addrはバインドアドレスである。
// rootは絶対パスとシンボリックリンク解決後のパスに事前解決される。
func NewServer(root, addr string, logger *slog.Logger) (*Server, error) {
	if root == "" {
		return nil, errors.New("root is required")
	}
	if addr == "" {
		return nil, errors.New("addr is required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create tftp root directory: %w", err)
	}

	resolvedRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve tftp root path: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(resolvedRoot)
	if err != nil {
		return nil, fmt.Errorf("evaluate symlinks in tftp root: %w", err)
	}

	return &Server{
		root:   realRoot,
		addr:   addr,
		logger: logger.With("component", "tftp"),
		done:   make(chan struct{}),
	}, nil
}

// Startはctxがキャンセルされるまでprocessをブロックし、TFTPサーバーを起動する。
func (s *Server) Start(ctx context.Context) error {
	lg := s.logger

	readHandler := func(filename string, rf io.ReaderFrom) error {
		lg.Info("TFTP read request", "filename", filename)
		file, err := openFile(s.root, filename, lg)
		if err != nil {
			lg.Error("failed to open TFTP file", "filename", filename, "error", err)
			return err
		}
		defer func() {
			if err := file.Close(); err != nil {
				lg.Error("failed to close TFTP file", "filename", filename, "error", err)
			}
		}()

		if _, err := rf.ReadFrom(file); err != nil {
			lg.Error("failed to read TFTP file", "filename", filename, "error", err)
			return err
		}
		return nil
	}

	server := tftp.NewServer(readHandler, nil)

	s.mu.Lock()
	s.server = server
	s.mu.Unlock()

	lg.Info("starting TFTP server", "address", s.addr, "root", s.root)

	udpAddr, err := net.ResolveUDPAddr("udp4", s.addr)
	if err != nil {
		return fmt.Errorf("resolve UDP address: %w", err)
	}
	conn, err := net.ListenUDP("udp4", udpAddr)
	if err != nil {
		return fmt.Errorf("listen UDP: %w", err)
	}

	s.mu.Lock()
	s.actualAddr = conn.LocalAddr().String()
	s.mu.Unlock()

	go func() {
		<-ctx.Done()
		if err := s.Stop(); err != nil {
			lg.Error("failed to stop TFTP server after context cancellation", "error", err)
		}
	}()

	serveErr := server.Serve(conn)
	if closeErr := conn.Close(); closeErr != nil {
		lg.Error("failed to close TFTP UDP connection", "address", s.addr, "error", closeErr)
	}
	close(s.done)

	if serveErr != nil && !errors.Is(serveErr, context.Canceled) && !errors.Is(serveErr, net.ErrClosed) {
		return fmt.Errorf("TFTP server exited: %w", serveErr)
	}
	return ctx.Err()
}

// Addrはサーバーの実際のリスニングアドレスを返す。起動前はコンストラクタへ渡されたアドレスを返す。
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.actualAddr != "" {
		return s.actualAddr
	}
	return s.addr
}

// Stopはサーバーを停止する。
func (s *Server) Stop() error {
	s.mu.Lock()
	server := s.server
	s.mu.Unlock()

	if server == nil {
		return nil
	}

	s.logger.Info("stopping TFTP server")
	server.Shutdown()
	<-s.done
	return nil
}
