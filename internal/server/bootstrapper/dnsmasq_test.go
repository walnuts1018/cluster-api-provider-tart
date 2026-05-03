package bootstrapper

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestNewDnsmasqBootstrapper(t *testing.T) {
	t.Run("ValidConfig", func(t *testing.T) {
		tmpDir := t.TempDir()
		b, err := NewDnsmasqBootstrapper(tmpDir, ":67")
		if err != nil {
			t.Fatalf("NewDnsmasqBootstrapper() error = %v", err)
		}
		if b.Addr() != ":67" {
			t.Errorf("Addr() = %q, want %q", b.Addr(), ":67")
		}
		if b.NeedLeaderElection() {
			t.Error("NeedLeaderElection() = true, want false")
		}
	})

	t.Run("EmptyTFTPRoot", func(t *testing.T) {
		_, err := NewDnsmasqBootstrapper("", ":67")
		if err == nil {
			t.Fatal("NewDnsmasqBootstrapper() expected error for empty tftpRoot")
		}
	})

	t.Run("EmptyAddr", func(t *testing.T) {
		tmpDir := t.TempDir()
		_, err := NewDnsmasqBootstrapper(tmpDir, "")
		if err == nil {
			t.Fatal("NewDnsmasqBootstrapper() expected error for empty addr")
		}
	})

	t.Run("CreateTFTPRoot", func(t *testing.T) {
		tmpDir := t.TempDir()
		nestedDir := filepath.Join(tmpDir, "nested", "tftp")
		b, err := NewDnsmasqBootstrapper(nestedDir, ":67")
		if err != nil {
			t.Fatalf("NewDnsmasqBootstrapper() error = %v", err)
		}
		_ = b
		if _, err := os.Stat(nestedDir); err != nil {
			t.Errorf("TFTP root directory not created: %v", err)
		}
	})
}

func TestExtractInterface(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		expected string
	}{
		{
			name:     "InterfaceWithPort",
			addr:     "eth0:67",
			expected: "eth0",
		},
		{
			name:     "AllInterfaces",
			addr:     "0.0.0.0:67",
			expected: "",
		},
		{
			name:     "IPv6AllInterfaces",
			addr:     "[::]:67",
			expected: "",
		},
		{
			name:     "JustPort",
			addr:     ":67",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractInterface(tt.addr)
			if got != tt.expected {
				t.Errorf("extractInterface(%q) = %q, want %q", tt.addr, got, tt.expected)
			}
		})
	}
}

func TestDnsmasqBootstrapper_Start(t *testing.T) {
	// dnsmasq がインストールされているかチェック
	if _, err := execCommand("dnsmasq", "--version"); err != nil {
		t.Skip("dnsmasq not installed, skipping test")
	}

	tmpDir := t.TempDir()

	// iPXE ローダファイルをダミーで作成
	iPXEPath := filepath.Join(tmpDir, iPXEBootFileName)
	if err := os.WriteFile(iPXEPath, []byte("dummy"), 0644); err != nil {
		t.Fatalf("failed to create dummy iPXE file: %v", err)
	}

	b, err := NewDnsmasqBootstrapper(tmpDir, ":0")
	if err != nil {
		t.Fatalf("NewDnsmasqBootstrapper() error = %v", err)
	}

	// Start はコンテキストキャンセルで終了することをテスト
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- b.Start(ctx)
	}()

	// 少し待ってからキャンセル
	time.Sleep(100 * time.Millisecond)
	cancel()

	// 終了待ち
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("Start() unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Start() did not exit after context cancellation")
	}
}

func TestDnsmasqBootstrapper_Start_WithoutIPXE(t *testing.T) {
	tmpDir := t.TempDir()

	b, err := NewDnsmasqBootstrapper(tmpDir, ":67")
	if err != nil {
		t.Fatalf("NewDnsmasqBootstrapper() error = %v", err)
	}

	ctx := context.Background()
	err = b.Start(ctx)
	if err == nil {
		t.Fatal("Start() expected error when iPXE bootloader is missing")
	}
}

func execCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(output), nil
}
