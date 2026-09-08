package dhcp

import (
	"bytes"
	"log/slog"
	"net"
	"testing"

	"github.com/insomniacslk/dhcp/dhcpv4"

	domainnetboot "github.com/walnuts1018/cluster-api-provider-tart/domain/netboot"
)

func TestClientArchitecture(t *testing.T) {
	t.Parallel()

	newRequest := func(t *testing.T, option []byte, includeArchitecture bool) *dhcpv4.DHCPv4 {
		t.Helper()
		modifiers := make([]dhcpv4.Modifier, 0, 1)
		if includeArchitecture {
			modifiers = append(modifiers, dhcpv4.WithGeneric(dhcpv4.OptionClientSystemArchitectureType, option))
		}
		request, err := dhcpv4.NewDiscovery(net.HardwareAddr{0, 0, 0x5e, 0, 0x53, 1}, modifiers...)
		if err != nil {
			t.Fatalf("NewDiscovery() error = %v", err)
		}
		return request
	}

	tests := []struct {
		name      string
		request   *dhcpv4.DHCPv4
		wantArch  uint16
		wantFound bool
	}{
		{name: "option absent", request: newRequest(t, nil, false)},
		{name: "short option", request: newRequest(t, []byte{0x00}, true), wantFound: false},
		{name: "big endian architecture", request: newRequest(t, []byte{0x00, byte(domainnetboot.ArchEFIx8664)}, true), wantArch: uint16(domainnetboot.ArchEFIx8664), wantFound: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotArch, gotFound := clientArchitecture(tt.request)
			if gotArch != tt.wantArch || gotFound != tt.wantFound {
				t.Fatalf("clientArchitecture() = (%d, %t), want (%d, %t)", gotArch, gotFound, tt.wantArch, tt.wantFound)
			}
		})
	}
}

func TestCreateHandlerReturnsPXEConfigurationOnlyForSupportedArchitecture(t *testing.T) {
	t.Parallel()

	server := &Server{
		baseURL:     "http://test.walnuts.dev:8080",
		advertiseIP: net.IPv4(192, 0, 2, 10),
		logger:      slog.New(slog.DiscardHandler),
	}
	handler := server.createHandler()

	newRequest := func(t *testing.T, architecture []byte) *dhcpv4.DHCPv4 {
		t.Helper()
		request, err := dhcpv4.NewDiscovery(net.HardwareAddr{0, 0, 0x5e, 0, 0x53, 2},
			dhcpv4.WithMessageType(dhcpv4.MessageTypeRequest),
			dhcpv4.WithGeneric(dhcpv4.OptionClientSystemArchitectureType, architecture),
		)
		if err != nil {
			t.Fatalf("NewDiscovery() error = %v", err)
		}
		return request
	}

	tests := []struct {
		name         string
		port         int
		architecture []byte
		wantWrites   int
		wantBoot     string
		wantType     dhcpv4.MessageType
	}{
		{name: "unsupported architecture is ignored", port: pxePort, architecture: []byte{0, byte(domainnetboot.ArchEFIARM64)}},
		{name: "amd64 PXE request receives an ACK and boot file", port: pxePort, architecture: []byte{0, byte(domainnetboot.ArchEFIx8664)}, wantWrites: 1, wantBoot: domainnetboot.IPXEBootFileNameAMD64, wantType: dhcpv4.MessageTypeAck},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			conn := &recordingPacketConn{localAddr: &net.UDPAddr{IP: net.IPv4zero, Port: tt.port}}
			handler(conn, &net.UDPAddr{IP: net.IPv4(192, 0, 2, 20), Port: 68}, newRequest(t, tt.architecture))
			if len(conn.writes) != tt.wantWrites {
				t.Fatalf("handler wrote %d responses, want %d", len(conn.writes), tt.wantWrites)
			}
			if tt.wantWrites == 0 {
				return
			}
			response, err := dhcpv4.FromBytes(conn.writes[0])
			if err != nil {
				t.Fatalf("FromBytes() error = %v", err)
			}
			if response.MessageType() != tt.wantType || response.BootFileName != tt.wantBoot {
				t.Fatalf("response = (type=%s, boot=%q), want (type=%s, boot=%q)", response.MessageType(), response.BootFileName, tt.wantType, tt.wantBoot)
			}
			if !bytes.Equal(response.GetOneOption(dhcpv4.OptionServerIdentifier), net.IPv4(192, 0, 2, 10).To4()) {
				t.Fatalf("server identifier = %v, want 192.0.2.10", response.GetOneOption(dhcpv4.OptionServerIdentifier))
			}
		})
	}
}

type recordingPacketConn struct {
	net.PacketConn
	localAddr net.Addr
	writes    [][]byte
}

func (c *recordingPacketConn) LocalAddr() net.Addr {
	return c.localAddr
}

func (c *recordingPacketConn) WriteTo(data []byte, _ net.Addr) (int, error) {
	c.writes = append(c.writes, bytes.Clone(data))
	return len(data), nil
}
