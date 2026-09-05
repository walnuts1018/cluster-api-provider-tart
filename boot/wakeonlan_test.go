package boot

import (
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
)

func TestMagicPacket(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		macAddress network.MACAddress
		wantErr    bool
		wantLen    int
	}{
		{
			name:       "有効なMACアドレス",
			macAddress: mustMACAddress(t, "00:00:5e:00:53:02"),
			wantLen:    magicPacketHeaderSize + 6*magicPacketRepeatCount,
		},
		{name: "未設定のMACアドレスはエラー", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			packet, err := magicPacket(tt.macAddress)
			if (err != nil) != tt.wantErr {
				t.Fatalf("magicPacket() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if len(packet) != tt.wantLen {
				t.Errorf("len(packet) = %d, want %d", len(packet), tt.wantLen)
			}
		})
	}
}

func mustMACAddress(t *testing.T, value string) network.MACAddress {
	t.Helper()
	address, err := network.ParseMACAddress(value)
	if err != nil {
		t.Fatalf("ParseMACAddress() error = %v", err)
	}
	return address
}
