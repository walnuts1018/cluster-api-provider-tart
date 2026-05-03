package wol_test

import (
	"bytes"
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/wol"
)

func TestMagicPacket(t *testing.T) {
	t.Parallel()

	packet, err := wol.MagicPacket("00:11:22:33:44:55")
	if err != nil {
		t.Fatalf("MagicPacket() error = %v", err)
	}

	want := append(bytes.Repeat([]byte{0xff}, 6), bytes.Repeat([]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55}, 16)...)
	if !bytes.Equal(packet, want) {
		t.Fatalf("MagicPacket() = %x, want %x", packet, want)
	}
}

func TestMagicPacketRejectsInvalidMACAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		macAddress string
	}{
		{name: "empty", macAddress: ""},
		{name: "not a mac address", macAddress: "not-a-mac-address"},
		{name: "too short", macAddress: "00:11:22:33:44"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := wol.MagicPacket(tt.macAddress)
			if err == nil {
				t.Fatal("MagicPacket() error = nil, want error")
			}
		})
	}
}
