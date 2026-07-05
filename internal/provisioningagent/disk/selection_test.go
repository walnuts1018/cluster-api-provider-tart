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

package disk

import (
	"errors"
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

func TestSelect(t *testing.T) {
	t.Parallel()

	expected := agentprotocol.RootDevice{
		DeviceName:   "/dev/disk/by-id/wwn-root",
		SerialNumber: "serial-root",
		WWN:          "wwn-root",
		MinSizeBytes: 100,
	}
	matching := Device{
		Path:         "/dev/nvme0n1",
		ByIDPaths:    []string{"/dev/disk/by-id/wwn-root"},
		SerialNumber: "serial-root",
		WWN:          "wwn-root",
		SizeBytes:    100,
	}

	tests := []struct {
		name    string
		devices []Device
		want    string
		wantErr error
	}{
		{name: "唯一の一致diskを選ぶ", devices: []Device{matching}, want: matching.Path},
		{name: "候補0台を拒否する", wantErr: ErrDiskIdentityMismatch},
		{name: "by-id不一致を拒否する", devices: []Device{with(matching, func(device *Device) {
			device.ByIDPaths = []string{"/dev/disk/by-id/other"}
		})}, wantErr: ErrDiskIdentityMismatch},
		{name: "serial不一致を拒否する", devices: []Device{with(matching, func(device *Device) {
			device.SerialNumber = "other"
		})}, wantErr: ErrDiskIdentityMismatch},
		{name: "WWN不一致を拒否する", devices: []Device{with(matching, func(device *Device) {
			device.WWN = "other"
		})}, wantErr: ErrDiskIdentityMismatch},
		{name: "容量不足を拒否する", devices: []Device{with(matching, func(device *Device) {
			device.SizeBytes = 99
		})}, wantErr: ErrDiskIdentityMismatch},
		{name: "Agent一時OSのdiskを拒否する", devices: []Device{with(matching, func(device *Device) {
			device.HoldsAgentOS = true
		})}, wantErr: ErrDiskIdentityMismatch},
		{name: "候補2台を拒否する", devices: []Device{matching, with(matching, func(device *Device) {
			device.Path = "/dev/nvme1n1"
		})}, wantErr: ErrDiskSelectionAmbiguous},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Select(expected, tt.devices)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Select() error = %v, want %v", err, tt.wantErr)
			}
			if got.Path != tt.want {
				t.Fatalf("Select() path = %q, want %q", got.Path, tt.want)
			}
		})
	}
}

func with(device Device, mutate func(*Device)) Device {
	mutate(&device)
	return device
}
