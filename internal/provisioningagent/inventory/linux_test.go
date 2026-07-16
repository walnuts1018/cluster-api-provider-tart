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

package inventory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/disk"
)

func TestLinuxCollectorCollectsWholeDisksAndAgentOSHolder(t *testing.T) {
	root := t.TempDir()
	paths := LinuxPaths{
		SysClassBlock: filepath.Join(root, "sys/class/block"),
		SysDevBlock:   filepath.Join(root, "sys/dev/block"),
		DevDiskByID:   filepath.Join(root, "dev/disk/by-id"),
		MountInfo:     filepath.Join(root, "proc/self/mountinfo"),
	}
	for _, directory := range []string{
		filepath.Join(paths.SysClassBlock, "sda/device"),
		filepath.Join(paths.SysClassBlock, "sda/slaves"),
		filepath.Join(paths.SysClassBlock, "sda1"),
		filepath.Join(paths.SysClassBlock, "nvme0n1/device"),
		filepath.Join(paths.SysClassBlock, "nvme0n1/slaves"),
		filepath.Join(paths.SysClassBlock, "loop0"),
		filepath.Join(paths.SysClassBlock, "sr0"),
		paths.SysDevBlock,
		paths.DevDiskByID,
		filepath.Dir(paths.MountInfo),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(paths.SysClassBlock, "sda/size"), "2097152\n")
	writeTestFile(t, filepath.Join(paths.SysClassBlock, "sda/device/serial"), "SERIAL-A\n")
	writeTestFile(t, filepath.Join(paths.SysClassBlock, "sda/device/wwid"), "WWN-A\n")
	writeTestFile(t, filepath.Join(paths.SysClassBlock, "sda1/partition"), "1\n")
	writeTestFile(t, filepath.Join(paths.SysClassBlock, "sda1/size"), "1048576\n")
	writeTestFile(t, filepath.Join(paths.SysClassBlock, "nvme0n1/size"), "4194304\n")
	writeTestFile(t, filepath.Join(paths.SysClassBlock, "loop0/size"), "0\n")
	writeTestFile(t, filepath.Join(paths.SysClassBlock, "sr0/size"), "0\n")
	writeTestFile(t, paths.MountInfo, "31 22 8:1 / / rw,relatime - ext4 /dev/sda1 rw\n")
	if err := os.Symlink("../../../devices/pci/block/sda/sda1", filepath.Join(paths.SysDevBlock, "8:1")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../sda", filepath.Join(paths.DevDiskByID, "wwn-a")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../nvme0n1", filepath.Join(paths.DevDiskByID, "nvme-b")); err != nil {
		t.Fatal(err)
	}

	devices, err := NewLinuxCollector(paths).Collect()
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("len(devices) = %d, want 2: %#v", len(devices), devices)
	}
	if devices[0].Path != "/dev/nvme0n1" || devices[0].HoldsAgentOS {
		t.Fatalf("devices[0] = %#v", devices[0])
	}
	if devices[1].Path != "/dev/sda" || !devices[1].HoldsAgentOS {
		t.Fatalf("devices[1] = %#v", devices[1])
	}
	if devices[1].ByIDPaths[0] != "/dev/disk/by-id/wwn-a" ||
		devices[1].SerialNumber != "SERIAL-A" ||
		devices[1].WWN != "WWN-A" ||
		devices[1].SizeBytes != 1<<30 {
		t.Fatalf("devices[1] identities = %#v", devices[1])
	}
}

func TestRootMountDevice(t *testing.T) {
	mountInfo := "20 1 0:19 / /proc rw - proc proc rw\n31 22 259:2 / / rw - ext4 /dev/nvme0n1p2 rw\n"
	got, ok := rootMountDevice(mountInfo)
	if !ok || got != "259:2" {
		t.Fatalf("rootMountDevice() = %q, %v", got, ok)
	}
}

func TestWholeDiskFromSysTarget(t *testing.T) {
	tests := map[string]string{
		"partition": "../../../devices/pci/block/nvme0n1/nvme0n1p2",
		"whole":     "../../../devices/virtual/block/dm-0",
	}
	for name, target := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := wholeDiskFromSysTarget(target)
			if !ok {
				t.Fatal("wholeDiskFromSysTarget() did not find block device")
			}
			want := map[string]string{"partition": "nvme0n1", "whole": "dm-0"}[name]
			if got != want {
				t.Fatalf("wholeDiskFromSysTarget() = %q, want %q", got, want)
			}
		})
	}
}

func TestToProtocolCopiesDiskSafetyInventory(t *testing.T) {
	devices := []disk.Device{{
		Path:         "/dev/sda",
		ByIDPaths:    []string{"/dev/disk/by-id/disk-a"},
		SerialNumber: "serial",
		WWN:          "wwn",
		SizeBytes:    1024,
		HoldsAgentOS: true,
	}}
	got := ToProtocol("system", "00:11:22:33:44:55", devices)
	if len(got.Disks) != 1 ||
		got.Disks[0].ByIDPaths[0] != devices[0].ByIDPaths[0] ||
		!got.Disks[0].HoldsAgentOS {
		t.Fatalf("ToProtocol() = %#v", got)
	}
}

func writeTestFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}
