package bootstrapper

import (
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
		bs, err := NewDHCPBootstrapper(tmpDir, ":67", "127.0.0.1", "http://127.0.0.1:8080")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if bs == nil {
			t.Fatal("expected non-nil bootstrapper")
		}
		if bs.addr != ":67" {
			t.Errorf("expected addr :67, got %s", bs.addr)
		}
		if bs.baseURL != "http://127.0.0.1:8080" {
			t.Errorf("expected baseURL http://127.0.0.1:8080, got %s", bs.baseURL)
		}
	})

	t.Run("empty tftpRoot", func(t *testing.T) {
		_, err := NewDHCPBootstrapper("", ":67", "127.0.0.1", "http://127.0.0.1:8080")
		if err == nil {
			t.Fatal("expected error for empty tftpRoot")
		}
		if want := "tftpRoot is required"; err.Error() != want {
			t.Errorf("expected error %q, got %q", want, err.Error())
		}
	})

	t.Run("empty addr", func(t *testing.T) {
		tmpDir := t.TempDir()
		_, err := NewDHCPBootstrapper(tmpDir, "", "127.0.0.1", "http://127.0.0.1:8080")
		if err == nil {
			t.Fatal("expected error for empty addr")
		}
		if want := "addr is required"; err.Error() != want {
			t.Errorf("expected error %q, got %q", want, err.Error())
		}
	})

	t.Run("empty baseURL", func(t *testing.T) {
		tmpDir := t.TempDir()
		_, err := NewDHCPBootstrapper(tmpDir, ":67", "127.0.0.1", "")
		if err == nil {
			t.Fatal("expected error for empty baseURL")
		}
		if want := "baseURL is required"; err.Error() != want {
			t.Errorf("expected error %q, got %q", want, err.Error())
		}
	})
}

func TestDHCPBootstrapper_Addr(t *testing.T) {
	tmpDir := t.TempDir()
	bs, err := NewDHCPBootstrapper(tmpDir, ":68", "127.0.0.1", "http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := bs.Addr(); got != ":68" {
		t.Errorf("expected addr :68, got %s", got)
	}
}

func TestDHCPBootstrapper_NeedLeaderElection(t *testing.T) {
	tmpDir := t.TempDir()
	bs, err := NewDHCPBootstrapper(tmpDir, ":67", "127.0.0.1", "http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := bs.NeedLeaderElection(); got != false {
		t.Errorf("expected NeedLeaderElection false, got %v", got)
	}
}

func TestResolveAdvertiseIP(t *testing.T) {
	t.Run("uses explicit advertise address for unspecified binds", func(t *testing.T) {
		got, err := ResolveAdvertiseIP(":67", ":8082", "127.0.0.1")
		if err != nil {
			t.Fatalf("ResolveAdvertiseIP returned error: %v", err)
		}
		if got.String() != "127.0.0.1" {
			t.Fatalf("advertiseIP = %q, want %q", got.String(), "127.0.0.1")
		}
	})

	t.Run("prefers explicit bind IP when available", func(t *testing.T) {
		got, err := ResolveAdvertiseIP("192.0.2.10:67", ":8082", "")
		if err != nil {
			t.Fatalf("ResolveAdvertiseIP returned error: %v", err)
		}
		if got.String() != "192.0.2.10" {
			t.Fatalf("advertiseIP = %q, want %q", got.String(), "192.0.2.10")
		}
	})
}

func TestDHCPBootstrapper_Start(t *testing.T) {
	tmpDir := t.TempDir()

	// iPXE ブートローダファイルを偽造
	iPXEPath := filepath.Join(tmpDir, iPXEBootFileNameAMD64)
	if err := os.WriteFile(iPXEPath, []byte("fake ipxe binary"), 0644); err != nil {
		t.Fatalf("failed to create fake iPXE file: %v", err)
	}

	ctx := t.Context()

	bs, err := NewDHCPBootstrapper(tmpDir, "127.0.0.1:0", "127.0.0.1", "http://127.0.0.1:8080")
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

	ctx := t.Context()

	bs, err := NewDHCPBootstrapper(tmpDir, "127.0.0.1:0", "127.0.0.1", "http://127.0.0.1:8080")
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

	ctx := t.Context()

	bs, err := NewDHCPBootstrapper(tmpDir, "invalid", "127.0.0.1", "http://127.0.0.1:8080")
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
	bs, err := NewDHCPBootstrapper(tmpDir, ":67", "127.0.0.1", "http://127.0.0.1:8080")
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

	ctx := t.Context()

	testPort := 6800
	bs, err := NewDHCPBootstrapper(tmpDir, fmt.Sprintf("127.0.0.1:%d", testPort), "127.0.0.1", "http://127.0.0.1:8080")
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

	mac, err := net.ParseMAC("00:00:5e:00:53:14")
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

		if !strings.Contains(bootFile, "http://127.0.0.1:8080/ipxe?mac=") {
			t.Errorf("expected HTTP URL in boot file, got %s", bootFile)
		}

		if !strings.Contains(bootFile, "00%3A00%3A5e%3A00%3A53%3A14") {
			t.Errorf("expected URL-encoded MAC address in boot file, got %s", bootFile)
		}
	})
}

func TestDHCPBootstrapper_ProxyMode_RespondsRegardlessOfServerID(t *testing.T) {
	tmpDir := t.TempDir()

	iPXEPath := filepath.Join(tmpDir, iPXEBootFileNameAMD64)
	if err := os.WriteFile(iPXEPath, []byte("fake ipxe binary"), 0644); err != nil {
		t.Fatalf("failed to create fake iPXE file: %v", err)
	}

	ctx := t.Context()

	testPort := 6801
	bs, err := NewDHCPBootstrapper(tmpDir, fmt.Sprintf("127.0.0.1:%d", testPort), "127.0.0.1", "http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := bs.StartWithContext(ctx); err != nil {
		t.Fatalf("failed to start DHCP server: %v", err)
	}
	defer bs.Stop()

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

	mac, err := net.ParseMAC("00:00:5e:00:53:14")
	if err != nil {
		t.Fatalf("failed to parse MAC: %v", err)
	}

	t.Run("should respond even when Server Identifier option is set", func(t *testing.T) {
		xid := dhcpv4.TransactionID{0x00, 0x00, 0x17, 0x3f}
		pkt, err := dhcpv4.New(
			dhcpv4.WithMessageType(dhcpv4.MessageTypeRequest),
			dhcpv4.WithHwAddr(mac),
			dhcpv4.WithTransactionID(xid),
			dhcpv4.WithOption(dhcpv4.OptServerIdentifier(net.ParseIP("192.168.1.1"))),
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
	})

	t.Run("should respond when Server Identifier option is not set", func(t *testing.T) {
		xid := dhcpv4.TransactionID{0x00, 0x00, 0x17, 0x40}
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
	})
}

func TestDHCPBootstrapper_DifferentArchitectures(t *testing.T) {
	tmpDir := t.TempDir()

	iPXEPath := filepath.Join(tmpDir, iPXEBootFileNameAMD64)
	if err := os.WriteFile(iPXEPath, []byte("fake ipxe binary"), 0644); err != nil {
		t.Fatalf("failed to create fake iPXE file: %v", err)
	}

	ctx := t.Context()

	testPort := 6802
	bs, err := NewDHCPBootstrapper(tmpDir, fmt.Sprintf("127.0.0.1:%d", testPort), "127.0.0.1", "http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := bs.StartWithContext(ctx); err != nil {
		t.Fatalf("failed to start DHCP server: %v", err)
	}
	defer bs.Stop()

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

	mac, err := net.ParseMAC("00:00:5e:00:53:15")
	if err != nil {
		t.Fatalf("failed to parse MAC: %v", err)
	}

	t.Run("arm64 EFI client receives arm64 boot file", func(t *testing.T) {
		xid := dhcpv4.TransactionID{0x00, 0x00, 0x18, 0x01}
		pkt, err := dhcpv4.New(
			dhcpv4.WithMessageType(dhcpv4.MessageTypeDiscover),
			dhcpv4.WithHwAddr(mac),
			dhcpv4.WithTransactionID(xid),
			dhcpv4.WithOption(dhcpv4.OptClassIdentifier("PXEClient")),
			dhcpv4.WithOption(dhcpv4.OptClientArch(iana.Arch(uint16(ArchEFIARM64)))),
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

		bootFile := reply.BootFileName
		if bootFile != iPXEBootFileNameARM64 {
			t.Errorf("expected boot file %s for arm64, got %s", iPXEBootFileNameARM64, bootFile)
		}
	})

	t.Run("unknown architecture receives default boot file", func(t *testing.T) {
		xid := dhcpv4.TransactionID{0x00, 0x00, 0x18, 0x02}
		pkt, err := dhcpv4.New(
			dhcpv4.WithMessageType(dhcpv4.MessageTypeDiscover),
			dhcpv4.WithHwAddr(mac),
			dhcpv4.WithTransactionID(xid),
			dhcpv4.WithOption(dhcpv4.OptClassIdentifier("PXEClient")),
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

		bootFile := reply.BootFileName
		if bootFile != iPXEBootFileNameAMD64 {
			t.Errorf("expected default boot file %s (ArchEFIx8664 default), got %s", iPXEBootFileNameAMD64, bootFile)
		}
	})
}
