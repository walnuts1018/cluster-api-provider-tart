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
