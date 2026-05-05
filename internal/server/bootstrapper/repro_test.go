package bootstrapper

import (
	"context"
	"net"
	"testing"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/iana"
)

func TestDHCPHandler_ProxyDHCP_Logic(t *testing.T) {
	tmpDir := t.TempDir()
	advertiseIP := "192.168.1.1"
	baseURL := "http://192.168.1.1:8080"
	bs, err := NewDHCPBootstrapper(tmpDir, ":67", advertiseIP, baseURL)
	if err != nil {
		t.Fatalf("failed to create bootstrapper: %v", err)
	}

	handler := bs.createDHCPHandler(context.Background())

	t.Run("Arch 0 (Legacy) should receive correct boot file", func(t *testing.T) {
		m, _ := dhcpv4.NewDiscovery(net.HardwareAddr{0x18, 0x03, 0x73, 0xe4, 0xb9, 0xe7})
		m.UpdateOption(dhcpv4.OptClientArch(iana.Arch(ArchIntelx86PC)))

		var response *dhcpv4.DHCPv4
		fakeConn := &fakePacketConn{
			onWriteTo: func(b []byte, addr net.Addr) (int, error) {
				var err error
				response, err = dhcpv4.FromBytes(b)
				return len(b), err
			},
			localAddr: &net.UDPAddr{Port: 67},
		}

		handler(fakeConn, &net.UDPAddr{IP: net.IPv4zero, Port: 68}, m)

		if response == nil {
			t.Fatal("expected a response")
		}

		if response.BootFileName != iPXEBootFileNameLegacy {
			t.Errorf("expected boot file %s, got %s", iPXEBootFileNameLegacy, response.BootFileName)
		}

		if response.ServerIPAddr.String() == "0.0.0.0" {
			t.Error("expected non-zero ServerIPAddr (siaddr)")
		}
	})
}

type fakePacketConn struct {
	net.PacketConn
	onWriteTo func(b []byte, addr net.Addr) (int, error)
	localAddr net.Addr
}

func (f *fakePacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	return f.onWriteTo(b, addr)
}

func (f *fakePacketConn) LocalAddr() net.Addr {
	return f.localAddr
}

func (f *fakePacketConn) Close() error { return nil }
