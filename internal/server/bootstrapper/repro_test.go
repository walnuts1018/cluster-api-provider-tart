// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bootstrapper

import (
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

	handler := bs.createDHCPHandler(t.Context())

	t.Run("Arch 0 (Legacy) on Port 67 is ignored", func(t *testing.T) {
		m, _ := dhcpv4.NewDiscovery(net.HardwareAddr{0x18, 0x03, 0x73, 0xe4, 0xb9, 0xe7})
		m.UpdateOption(dhcpv4.OptClientArch(iana.Arch(ArchIntelx86PC)))

		var response *dhcpv4.DHCPv4
		fakeConn := &fakePacketConn{
			onWriteTo: func(b []byte, addr net.Addr) (int, error) {
				var err error
				response, err = dhcpv4.FromBytes(b)
				return len(b), err
			},
			localAddr: &net.UDPAddr{Port: dhcpPort},
		}

		handler(fakeConn, &net.UDPAddr{IP: net.IPv4zero, Port: 68}, m)

		if response != nil {
			t.Fatalf("unexpected ProxyDHCP response for unsupported architecture: %v", response)
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
