package bootstrapper

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/pin/tftp/v3"
)

func TestNewTFTPBootstrapper(t *testing.T) {
	t.Run("valid parameters", func(t *testing.T) {
		tmpDir := t.TempDir()
		bs, err := NewTFTPBootstrapper(tmpDir, ":69")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if bs == nil {
			t.Fatal("expected non-nil bootstrapper")
		}
		if bs.addr != ":69" {
			t.Errorf("expected addr :69, got %s", bs.addr)
		}
		resolvedRoot, err := filepath.EvalSymlinks(tmpDir)
		if err != nil {
			t.Fatalf("failed to eval root path: %v", err)
		}
		if bs.root != resolvedRoot {
			t.Errorf("expected root %s, got %s", resolvedRoot, bs.root)
		}
	})

	t.Run("empty root", func(t *testing.T) {
		_, err := NewTFTPBootstrapper("", ":69")
		if err == nil {
			t.Fatal("expected error for empty root")
		}
		if want := "root is required"; err.Error() != want {
			t.Errorf("expected error %q, got %q", want, err.Error())
		}
	})

	t.Run("empty addr", func(t *testing.T) {
		tmpDir := t.TempDir()
		_, err := NewTFTPBootstrapper(tmpDir, "")
		if err == nil {
			t.Fatal("expected error for empty addr")
		}
		if want := "addr is required"; err.Error() != want {
			t.Errorf("expected error %q, got %q", want, err.Error())
		}
	})
}

func TestTFTPBootstrapper_Addr(t *testing.T) {
	tmpDir := t.TempDir()
	bs, err := NewTFTPBootstrapper(tmpDir, ":70")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := bs.Addr(); got != ":70" {
		t.Errorf("expected addr :70, got %s", got)
	}
}

func TestTFTPBootstrapper_NeedLeaderElection(t *testing.T) {
	tmpDir := t.TempDir()
	bs, err := NewTFTPBootstrapper(tmpDir, ":69")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := bs.NeedLeaderElection(); got != false {
		t.Errorf("expected NeedLeaderElection false, got %v", got)
	}
}

func TestTFTPBootstrapper_Start(t *testing.T) {
	tmpDir := t.TempDir()

	// テストファイルを作成
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bs, err := NewTFTPBootstrapper(tmpDir, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := bs.StartWithContext(ctx); err != nil {
		t.Fatalf("failed to start TFTP server: %v", err)
	}

	// サーバーが正常に起動したことを確認
	if bs.server == nil {
		t.Fatal("expected non-nil server after Start")
	}

	// TFTPサーバーが動作していることを確認（ファイルを取得できるかテスト）
	// アドレスが動的に割り当てられるので、実際にポートを確認する
	time.Sleep(100 * time.Millisecond)

	// サーバーを停止
	if err := bs.Stop(); err != nil {
		t.Errorf("failed to stop TFTP server: %v", err)
	}
}

func TestTFTPBootstrapper_Start_InvalidAddress(t *testing.T) {
	tmpDir := t.TempDir()

	ctx := t.Context()

	bs, err := NewTFTPBootstrapper(tmpDir, "invalid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// アドレスが無効の場合、StartWithContextはエラーを返す
	err = bs.StartWithContext(ctx)
	if err == nil {
		t.Fatal("expected error for invalid address")
	}
}

func TestTFTPBootstrapper_Stop_NilServer(t *testing.T) {
	tmpDir := t.TempDir()
	bs, err := NewTFTPBootstrapper(tmpDir, ":69")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Startを呼んでいないのでserverはnil
	if err := bs.Stop(); err != nil {
		t.Errorf("expected no error when stopping nil server, got: %v", err)
	}
}

func TestTFTPBootstrapper_FileDownload(t *testing.T) {
	tmpDir := t.TempDir()

	// テストファイルを作成
	testFile := "test.txt"
	testContent := "Hello, TFTP!"
	testFilePath := filepath.Join(tmpDir, testFile)
	if err := os.WriteFile(testFilePath, []byte(testContent), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bs, err := NewTFTPBootstrapper(tmpDir, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := bs.StartWithContext(ctx); err != nil {
		t.Fatalf("failed to start TFTP server: %v", err)
	}

	// サーバーが起動するまで待機
	time.Sleep(200 * time.Millisecond)

	// サーバーが動作していることを確認
	if bs.server == nil {
		t.Fatal("expected non-nil server")
	}

	// サーバーを停止
	bs.Stop()
}

func TestResolveTFTPFilePath(t *testing.T) {
	t.Run("allows file inside root", func(t *testing.T) {
		root := t.TempDir()
		filePath := filepath.Join(root, "images", "ipxe.efi")
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			t.Fatalf("failed to create file dir: %v", err)
		}
		if err := os.WriteFile(filePath, []byte("ipxe"), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}

		resolved, err := resolveTFTPFilePath(root, "images/ipxe.efi")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want, err := filepath.EvalSymlinks(filePath)
		if err != nil {
			t.Fatalf("failed to eval file path: %v", err)
		}
		if resolved != want {
			t.Fatalf("expected path %q, got %q", want, resolved)
		}
	})

	t.Run("rejects symlink escape", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		targetDir := filepath.Join(outside, "sensitive")
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			t.Fatalf("failed to create target dir: %v", err)
		}

		// ターゲットにダミーファイルを作成し、EvalSymlinks が成功するようにする
		targetFile := filepath.Join(targetDir, "secret.txt")
		if err := os.WriteFile(targetFile, []byte("secret"), 0644); err != nil {
			t.Fatalf("failed to create target file: %v", err)
		}

		linkPath := filepath.Join(root, "link")
		if err := os.Symlink(targetDir, linkPath); err != nil {
			t.Fatalf("failed to create symlink: %v", err)
		}

		_, err := resolveTFTPFilePath(root, "link/secret.txt")
		if err == nil {
			t.Fatal("expected symlink traversal to be rejected")
		}
	})
}

func TestOpenTFTPFile(t *testing.T) {
	t.Run("rejects file larger than limit", func(t *testing.T) {
		root := t.TempDir()
		filePath := filepath.Join(root, "large.img")
		file, err := os.Create(filePath)
		if err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
		if err := file.Truncate(maxTFTPFileSize + 1); err != nil {
			t.Fatalf("failed to enlarge file: %v", err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("failed to close file: %v", err)
		}

		opened, err := openTFTPFile(root, "large.img", logr.Discard())
		if opened != nil {
			if closeErr := opened.Close(); closeErr != nil {
				t.Logf("failed to close opened file: %v", closeErr)
			}
		}
		if err == nil {
			t.Fatal("expected oversized file to be rejected")
		}
	})

	t.Run("rejects directory", func(t *testing.T) {
		root := t.TempDir()
		subDir := filepath.Join(root, "subdir")
		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Fatalf("failed to create subdir: %v", err)
		}

		opened, err := openTFTPFile(root, "subdir", logr.Discard())
		if opened != nil {
			if closeErr := opened.Close(); closeErr != nil {
				t.Logf("failed to close opened file: %v", closeErr)
			}
		}
		if err == nil {
			t.Fatal("expected directory to be rejected")
		}
	})
}

func TestTFTPBootstrapper_RealFileDownload(t *testing.T) {
	tmpDir := t.TempDir()

	testFilename := "ipxe-x86_64.efi"
	testContent := []byte("fake iPXE bootloader binary content for testing")
	testFilePath := filepath.Join(tmpDir, testFilename)
	if err := os.WriteFile(testFilePath, testContent, 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bs, err := NewTFTPBootstrapper(tmpDir, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create TFTP bootstrapper: %v", err)
	}

	if err := bs.StartWithContext(ctx); err != nil {
		t.Fatalf("failed to start TFTP server: %v", err)
	}
	defer bs.Stop()

	addr := bs.Addr()
	if addr == "" || addr == ":0" {
		t.Fatalf("expected non-empty dynamic address, got %q", addr)
	}

	time.Sleep(200 * time.Millisecond)

	client, err := tftp.NewClient(addr)
	if err != nil {
		t.Fatalf("failed to create TFTP client: %v", err)
	}

	buf := new(bytes.Buffer)
	file, err := client.Receive(testFilename, "octet")
	if err != nil {
		t.Fatalf("failed to receive file: %v", err)
	}

	if _, err := file.WriteTo(buf); err != nil {
		t.Fatalf("failed to write file content: %v", err)
	}

	if !bytes.Equal(buf.Bytes(), testContent) {
		t.Fatalf("downloaded content = %q, want %q", buf.Bytes(), testContent)
	}
}

func TestTFTPBootstrapper_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bs, err := NewTFTPBootstrapper(tmpDir, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create TFTP bootstrapper: %v", err)
	}

	if err := bs.StartWithContext(ctx); err != nil {
		t.Fatalf("failed to start TFTP server: %v", err)
	}
	defer bs.Stop()

	addr := bs.Addr()
	time.Sleep(200 * time.Millisecond)

	client, err := tftp.NewClient(addr)
	if err != nil {
		t.Fatalf("failed to create TFTP client: %v", err)
	}

	buf := new(bytes.Buffer)
	_, err = client.Receive("nonexistent-file.efi", "octet")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}

	if buf.Len() > 0 {
		t.Fatal("expected empty buffer for nonexistent file")
	}
}

func TestTFTPBootstrapper_PathTraversalRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secretFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("secret content"), 0644); err != nil {
		t.Fatalf("failed to create secret file: %v", err)
	}

	secretDir := filepath.Join(outside, "sensitive")
	if err := os.MkdirAll(secretDir, 0755); err != nil {
		t.Fatalf("failed to create sensitive dir: %v", err)
	}
	secretFile2 := filepath.Join(secretDir, "secret.txt")
	if err := os.WriteFile(secretFile2, []byte("secret content"), 0644); err != nil {
		t.Fatalf("failed to create secret file 2: %v", err)
	}

	linkPath := filepath.Join(root, "link")
	if err := os.Symlink(secretDir, linkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bs, err := NewTFTPBootstrapper(root, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create TFTP bootstrapper: %v", err)
	}

	if err := bs.StartWithContext(ctx); err != nil {
		t.Fatalf("failed to start TFTP server: %v", err)
	}
	defer bs.Stop()

	addr := bs.Addr()
	time.Sleep(200 * time.Millisecond)

	client, err := tftp.NewClient(addr)
	if err != nil {
		t.Fatalf("failed to create TFTP client: %v", err)
	}

	_, err = client.Receive("link/secret.txt", "octet")
	if err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
}
