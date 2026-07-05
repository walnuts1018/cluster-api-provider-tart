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
