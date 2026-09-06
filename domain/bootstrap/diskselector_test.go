package bootstrap

import (
	"errors"
	"testing"
)

func TestSelectDisk(t *testing.T) {
	t.Parallel()

	base := DiskIdentity{
		DevicePath: "/dev/vda",
		SizeBytes:  64 * 1024 * 1024 * 1024,
		Model:      "TART DISK",
		Serial:     "disk-a",
		WWID:       "wwid-a",
		BusPath:    "pci-0000:00:05.0",
		Transport:  "virtio",
	}

	tests := map[string]struct {
		disks []DiskIdentity
		want  DiskIdentity
		err   error
	}{
		"unique stable disk": {
			disks: []DiskIdentity{base, {DevicePath: "/dev/vdb", SizeBytes: 128 * 1024 * 1024 * 1024, Model: "TART DATA", Serial: "disk-b", Transport: "virtio"}},
			want:  base,
		},
		"single disk without transport metadata": {
			disks: []DiskIdentity{{DevicePath: base.DevicePath, SizeBytes: base.SizeBytes, Serial: base.Serial}},
			want:  DiskIdentity{DevicePath: base.DevicePath, SizeBytes: base.SizeBytes, Serial: base.Serial},
		},
		"ambiguous disk identity": {
			disks: []DiskIdentity{base, {DevicePath: "/dev/vdb", SizeBytes: base.SizeBytes, Model: base.Model, Serial: base.Serial, WWID: base.WWID, BusPath: base.BusPath, Transport: base.Transport}},
			err:   ErrDiskSelectionAmbiguous,
		},
		"no writable disk": {
			disks: []DiskIdentity{{DevicePath: "/dev/vda", SizeBytes: base.SizeBytes, Transport: "virtio", ReadOnly: true}},
			err:   ErrDiskSelectionUnavailable,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := SelectDisk(tt.disks)
			if tt.err != nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("SelectDisk() error = %v, want %v", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("SelectDisk() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("SelectDisk() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestUniqueDiskSelectorPicksDistinctExpressionsは、複数diskから一意なselectorを導出でき、
// 各diskの選択結果が互いに異なることを検証する。TartHost.status.inventoryのstableSelector
// previewが、system diskと追加diskを区別できることの基礎になる。
func TestUniqueDiskSelectorPicksDistinctExpressions(t *testing.T) {
	t.Parallel()

	nvme := DiskIdentity{DevicePath: "/dev/vda", SizeBytes: 64 * 1024 * 1024 * 1024, Serial: "disk-a", Transport: "virtio"}
	ssd := DiskIdentity{DevicePath: "/dev/vdb", SizeBytes: 32 * 1024 * 1024 * 1024, Serial: "disk-b", Transport: "virtio"}
	hdd1 := DiskIdentity{DevicePath: "/dev/vdc", SizeBytes: 1024 * 1024 * 1024 * 1024, Serial: "disk-c", Transport: "virtio", Rotational: true}
	hdd2 := DiskIdentity{DevicePath: "/dev/vdd", SizeBytes: 1024 * 1024 * 1024 * 1024, Serial: "disk-d", Transport: "virtio", Rotational: true}
	all := []DiskIdentity{nvme, ssd, hdd1, hdd2}

	seen := make(map[string]bool, len(all))
	for _, disk := range all {
		expression, ok := UniqueDiskSelector(disk, all)
		if !ok {
			t.Fatalf("UniqueDiskSelector() could not uniquely identify disk %+v", disk)
		}
		if seen[expression] {
			t.Fatalf("UniqueDiskSelector() returned a non-distinct expression %q for disk %+v", expression, disk)
		}
		seen[expression] = true
	}
}

// TestUniqueDiskSelectorAmbiguousは、区別できない2台のdiskに対してok=falseを返すことを検証する。
func TestUniqueDiskSelectorAmbiguous(t *testing.T) {
	t.Parallel()

	left := DiskIdentity{DevicePath: "/dev/vda", SizeBytes: 64 * 1024 * 1024 * 1024, Transport: "virtio"}
	right := DiskIdentity{DevicePath: "/dev/vdb", SizeBytes: 64 * 1024 * 1024 * 1024, Transport: "virtio"}

	if _, ok := UniqueDiskSelector(left, []DiskIdentity{left, right}); ok {
		t.Fatal("UniqueDiskSelector() unexpectedly resolved an ambiguous disk pair")
	}
}
