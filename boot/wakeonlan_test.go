package boot

import "testing"

func TestMagicPacket(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		macAddress string
		wantErr    bool
		wantLen    int
	}{
		{
			name:       "有効なMACアドレス",
			macAddress: "00:11:22:33:44:55",
			wantLen:    magicPacketHeaderSize + 6*magicPacketRepeatCount,
		},
		{
			name:       "不正なMACアドレスはエラー",
			macAddress: "not-a-mac",
			wantErr:    true,
		},
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
