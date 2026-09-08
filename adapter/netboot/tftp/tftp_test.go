package tftp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenFileRestrictsTFTPRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ipxe.efi"), []byte("bootloader"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	outside := filepath.Join(filepath.Dir(root), "outside")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatalf("WriteFile(outside) error = %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "directory"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	logger := slog.New(slog.DiscardHandler)
	tests := []struct {
		name       string
		filename   string
		want       string
		wantErr    bool
		errMessage string
	}{
		{name: "regular file", filename: "ipxe.efi", want: "bootloader"},
		{name: "parent traversal", filename: "../outside", wantErr: true, errMessage: "path traversal"},
		{name: "symlink escape", filename: "link", wantErr: true, errMessage: "path traversal"},
		{name: "directory", filename: "directory", wantErr: true, errMessage: "not a regular file"},
		{name: "missing file", filename: "missing", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			file, err := openFile(root, tt.filename, logger)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("openFile(%q) error = nil, want error", tt.filename)
				}
				if tt.errMessage != "" && !strings.Contains(err.Error(), tt.errMessage) {
					t.Errorf("openFile(%q) error = %v, want %q", tt.filename, err, tt.errMessage)
				}
				return
			}
			if err != nil {
				t.Fatalf("openFile(%q) error = %v", tt.filename, err)
			}
			defer func() {
				if closeErr := file.Close(); closeErr != nil {
					t.Errorf("Close() error = %v", closeErr)
				}
			}()
			data, err := io.ReadAll(file)
			if err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}
			if string(data) != tt.want {
				t.Errorf("file contents = %q, want %q", data, tt.want)
			}
		})
	}
}

func TestOpenFileRejectsOversizedFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "oversized")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := file.Truncate(maxFileSize + 1); err != nil {
		_ = file.Close()
		t.Fatalf("Truncate() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	opened, err := openFile(root, "oversized", slog.Default())
	if err == nil || !strings.Contains(err.Error(), "exceeds TFTP size limit") {
		t.Fatalf("openFile() error = %v, want size limit error", err)
	}
	if opened != nil {
		if closeErr := opened.Close(); closeErr != nil {
			t.Errorf("Close() error = %v", closeErr)
		}
	}
}

func TestNewServerResolvesRootAndReportsConfiguredAddressBeforeStart(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "tftp-root")
	server, err := NewServer(root, "127.0.0.1:1069", nil)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	if got := server.Addr(); got != "127.0.0.1:1069" {
		t.Errorf("Addr() before Start = %q, want configured address", got)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		t.Errorf("NewServer() did not create TFTP root directory: info=%v, error=%v", info, err)
	}
}

func TestStartStopsWhenContextIsCanceled(t *testing.T) {
	t.Parallel()

	server, err := NewServer(t.TempDir(), "127.0.0.1:0", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- server.Start(ctx)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for server.Addr() == "127.0.0.1:0" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if server.Addr() == "127.0.0.1:0" {
		cancel()
		t.Fatal("TFTP server did not publish an ephemeral address")
	}
	if _, _, err := net.SplitHostPort(server.Addr()); err != nil {
		t.Fatalf("TFTP server address = %q, error = %v", server.Addr(), err)
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Start() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("TFTP server did not stop after context cancellation")
	}
}

func TestStartClosesDoneWhenListenFails(t *testing.T) {
	t.Parallel()

	server, err := NewServer(t.TempDir(), "not-an-address", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}

	err = server.Start(t.Context())
	if err == nil {
		t.Fatal("Start() error = nil, want address resolution error")
	}

	stopped := make(chan struct{})
	go func() {
		if stopErr := server.Stop(); stopErr != nil {
			t.Errorf("Stop() error = %v", stopErr)
		}
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop() did not return after Start() failed")
	}
}
