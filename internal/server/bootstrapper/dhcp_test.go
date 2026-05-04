package bootstrapper

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/iana"
)

func TestNewDHCPBootstrapper(t *testing.T) {
	t.Run("valid parameters", func(t *testing.T) {
		tmpDir := t.TempDir()
		bs, err := NewDHCPBootstrapper(tmpDir, ":67", "127.0.0.1:8080")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if bs == nil {
			t.Fatal("expected non-nil bootstrapper")
		}
		if bs.addr != ":67" {
			t.Errorf("expected addr :67, got %s", bs.addr)
		}
		if bs.httpAddr != "127.0.0.1:8080" {
			t.Errorf("expected httpAddr 127.0.0.1:8080, got %s", bs.httpAddr)
		}
	})

	t.Run("empty tftpRoot", func(t *testing.T) {
		_, err := NewDHCPBootstrapper("", ":67", "127.0.0.1:8080")
		if err == nil {
			t.Fatal("expected error for empty tftpRoot")
		}
		if want := "tftpRoot is required"; err.Error() != want {
			t.Errorf("expected error %q, got %q", want, err.Error())
		}
	})

	t.Run("empty addr", func(t *testing.T) {
		tmpDir := t.TempDir()
		_, err := NewDHCPBootstrapper(tmpDir, "", "127.0.0.1:8080")
		if err == nil {
			t.Fatal("expected error for empty addr")
		}
		if want := "addr is required"; err.Error() != want {
			t.Errorf("expected error %q, got %q", want, err.Error())
		}
	})

	t.Run("empty httpAddr", func(t *testing.T) {
		tmpDir := t.TempDir()
		_, err := NewDHCPBootstrapper(tmpDir, ":67", "")
		if err == nil {
			t.Fatal("expected error for empty httpAddr")
		}
		if want := "httpAddr is required"; err.Error() != want {
			t.Errorf("expected error %q, got %q", want, err.Error())
		}
	})
}

func TestDHCPBootstrapper_Addr(t *testing.T) {
	tmpDir := t.TempDir()
	bs, err := NewDHCPBootstrapper(tmpDir, ":68", "127.0.0.1:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := bs.Addr(); got != ":68" {
		t.Errorf("expected addr :68, got %s", got)
	}
}

func TestDHCPBootstrapper_NeedLeaderElection(t *testing.T) {
	tmpDir := t.TempDir()
	bs, err := NewDHCPBootstrapper(tmpDir, ":67", "127.0.0.1:8080")
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

	bs, err := NewDHCPBootstrapper(tmpDir, "127.0.0.1:0", "127.0.0.1:8080")
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

	bs, err := NewDHCPBootstrapper(tmpDir, "127.0.0.1:0", "127.0.0.1:8080")
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

	bs, err := NewDHCPBootstrapper(tmpDir, "invalid", "127.0.0.1:8080")
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
	bs, err := NewDHCPBootstrapper(tmpDir, ":67", "127.0.0.1:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Startを呼んでいないのでserverはnil
	if err := bs.Stop(); err != nil {
		t.Errorf("expected no error when stopping nil server, got: %v", err)
	}
}

func TestDHCPBootstrapper_NextServerAndFileURI(t *testing.T) {
	tmpDir := t.TempDir()

	iPXEPath := filepath.Join(tmpDir, iPXEBootFileNameAMD64)
	if err := os.WriteFile(iPXEPath, []byte("fake ipxe binary"), 0644); err != nil {
		t.Fatalf("failed to create fake iPXE file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testPort := 6800
	bs, err := NewDHCPBootstrapper(tmpDir, fmt.Sprintf("127.0.0.1:%d", testPort), "127.0.0.1:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := bs.StartWithContext(ctx); err != nil {
		t.Fatalf("failed to start DHCP server: %v", err)
	}
	defer bs.Stop()

	serverAddr := bs.Addr()
	t.Logf("DHCP server listening on %s", serverAddr)

	localAddr, err := net.ResolveUDPAddr("udp4", ":0")
	if err != nil {
		t.Fatalf("failed to resolve local address: %v", err)
	}
	remoteAddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("127.0.0.1:%d", testPort))
	if err != nil {
		t.Fatalf("failed to resolve remote address: %v", err)
	}
	conn, err := net.DialUDP("udp4", localAddr, remoteAddr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	mac, err := net.ParseMAC("12:34:56:78:9a:bc")
	if err != nil {
		t.Fatalf("failed to parse MAC: %v", err)
	}

	t.Run("normal PXE client receives TFTP boot file", func(t *testing.T) {
		xid := dhcpv4.TransactionID{0x00, 0x00, 0x04, 0xd2}
		pkt, err := dhcpv4.New(
			dhcpv4.WithMessageType(dhcpv4.MessageTypeDiscover),
			dhcpv4.WithHwAddr(mac),
			dhcpv4.WithTransactionID(xid),
			dhcpv4.WithOption(dhcpv4.OptClassIdentifier("PXEClient")),
			dhcpv4.WithOption(dhcpv4.OptClientArch(iana.Arch(uint16(ArchEFIx8664)))),
		)
		if err != nil {
			t.Fatalf("failed to create packet: %v", err)
		}

		if _, err := conn.Write(pkt.ToBytes()); err != nil {
			t.Fatalf("failed to send packet: %v", err)
		}

		resp := make([]byte, 1500)
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, _, err := conn.ReadFrom(resp)
		if err != nil {
			t.Fatalf("failed to receive response: %v", err)
		}

		reply, err := dhcpv4.FromBytes(resp[:n])
		if err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if reply.MessageType() != dhcpv4.MessageTypeOffer {
			t.Errorf("expected MessageTypeOffer, got %s", reply.MessageType())
		}

		bootFile := reply.BootFileName
		t.Logf("Boot file: %s", bootFile)

		if bootFile != iPXEBootFileNameAMD64 {
			t.Errorf("expected boot file %s, got %s", iPXEBootFileNameAMD64, bootFile)
		}

		nextServer := reply.ServerIdentifier()
		t.Logf("Next server: %s", nextServer)
	})

	t.Run("iPXE client receives HTTP URL", func(t *testing.T) {
		xid := dhcpv4.TransactionID{0x00, 0x00, 0x16, 0x2e}
		pkt, err := dhcpv4.New(
			dhcpv4.WithMessageType(dhcpv4.MessageTypeDiscover),
			dhcpv4.WithHwAddr(mac),
			dhcpv4.WithTransactionID(xid),
			dhcpv4.WithOption(dhcpv4.OptClassIdentifier("PXEClient")),
			dhcpv4.WithOption(dhcpv4.OptClientArch(iana.Arch(uint16(ArchEFIx8664)))),
			dhcpv4.WithOption(dhcpv4.OptUserClass("iPXE")),
		)
		if err != nil {
			t.Fatalf("failed to create packet: %v", err)
		}

		if _, err := conn.Write(pkt.ToBytes()); err != nil {
			t.Fatalf("failed to send packet: %v", err)
		}

		resp := make([]byte, 1500)
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, _, err := conn.ReadFrom(resp)
		if err != nil {
			t.Fatalf("failed to receive response: %v", err)
		}

		reply, err := dhcpv4.FromBytes(resp[:n])
		if err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if reply.MessageType() != dhcpv4.MessageTypeOffer {
			t.Errorf("expected MessageTypeOffer, got %s", reply.MessageType())
		}

		bootFile := reply.BootFileName
		t.Logf("Boot file (iPXE): %s", bootFile)

		if !strings.Contains(bootFile, "http://127.0.0.1/ipxe?mac=") {
			t.Errorf("expected HTTP URL in boot file, got %s", bootFile)
		}

		if !strings.Contains(bootFile, "12%3A34%3A56%3A78%3A9a%3Abc") {
			t.Errorf("expected URL-encoded MAC address in boot file, got %s", bootFile)
		}
	})
}
