package bootstrapper

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewDHCPBootstrapper(t *testing.T) {
	t.Run("valid parameters", func(t *testing.T) {
		tmpDir := t.TempDir()
		bs, err := NewDHCPBootstrapper(tmpDir, ":67")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if bs == nil {
			t.Fatal("expected non-nil bootstrapper")
		}
		if bs.addr != ":67" {
			t.Errorf("expected addr :67, got %s", bs.addr)
		}
	})

	t.Run("empty tftpRoot", func(t *testing.T) {
		_, err := NewDHCPBootstrapper("", ":67")
		if err == nil {
			t.Fatal("expected error for empty tftpRoot")
		}
		if want := "tftpRoot is required"; err.Error() != want {
			t.Errorf("expected error %q, got %q", want, err.Error())
		}
	})

	t.Run("empty addr", func(t *testing.T) {
		tmpDir := t.TempDir()
		_, err := NewDHCPBootstrapper(tmpDir, "")
		if err == nil {
			t.Fatal("expected error for empty addr")
		}
		if want := "addr is required"; err.Error() != want {
			t.Errorf("expected error %q, got %q", want, err.Error())
		}
	})
}

func TestDHCPBootstrapper_Addr(t *testing.T) {
	tmpDir := t.TempDir()
	bs, err := NewDHCPBootstrapper(tmpDir, ":68")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := bs.Addr(); got != ":68" {
		t.Errorf("expected addr :68, got %s", got)
	}
}

func TestDHCPBootstrapper_NeedLeaderElection(t *testing.T) {
	tmpDir := t.TempDir()
	bs, err := NewDHCPBootstrapper(tmpDir, ":67")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := bs.NeedLeaderElection(); got != false {
		t.Errorf("expected NeedLeaderElection false, got %v", got)
	}
}

func TestDHCPBootstrapper_Start(t *testing.T) {
	tmpDir := t.TempDir()

	// iPXE ブートローダファイルを偽造
	iPXEPath := filepath.Join(tmpDir, iPXEBootFileNameAMD64)
	if err := os.WriteFile(iPXEPath, []byte("fake ipxe binary"), 0644); err != nil {
		t.Fatalf("failed to create fake iPXE file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bs, err := NewDHCPBootstrapper(tmpDir, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := bs.StartWithContext(ctx); err != nil {
		t.Fatalf("failed to start DHCP server: %v", err)
	}

	// サーバーが正常に起動したことを確認
	if bs.server == nil {
		t.Fatal("expected non-nil server after Start")
	}

	// サーバーを停止
	if err := bs.Stop(); err != nil {
		t.Errorf("failed to stop DHCP server: %v", err)
	}
}

func TestDHCPBootstrapper_Start_WithoutIPXE(t *testing.T) {
	tmpDir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bs, err := NewDHCPBootstrapper(tmpDir, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := bs.StartWithContext(ctx); err != nil {
		t.Fatalf("failed to start DHCP server without iPXE file: %v", err)
	}

	if bs.server == nil {
		t.Fatal("expected non-nil server after Start")
	}
}

func TestDHCPBootstrapper_Start_InvalidAddress(t *testing.T) {
	tmpDir := t.TempDir()

	// iPXE ブートローダファイルを偽造
	iPXEPath := filepath.Join(tmpDir, iPXEBootFileNameAMD64)
	if err := os.WriteFile(iPXEPath, []byte("fake ipxe binary"), 0644); err != nil {
		t.Fatalf("failed to create fake iPXE file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bs, err := NewDHCPBootstrapper(tmpDir, "invalid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = bs.StartWithContext(ctx)
	if err == nil {
		t.Fatal("expected error for invalid address")
	}
	if want := "invalid bind address"; !strings.Contains(err.Error(), want) {
		t.Errorf("expected error containing %q, got %q", want, err.Error())
	}
}

func TestDHCPBootstrapper_Stop_NilServer(t *testing.T) {
	tmpDir := t.TempDir()
	bs, err := NewDHCPBootstrapper(tmpDir, ":67")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Startを呼んでいないのでserverはnil
	if err := bs.Stop(); err != nil {
		t.Errorf("expected no error when stopping nil server, got: %v", err)
	}
}
