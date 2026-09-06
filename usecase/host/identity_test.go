package host

import (
	"testing"

	"k8s.io/apimachinery/pkg/types"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
)

func TestHasIdentityConflict(t *testing.T) {
	t.Parallel()

	base := infrav1alpha1.TartHost{
		Name: "host-a",
		UID:  types.UID("a"),
		Spec: infrav1alpha1.TartHostSpec{MACAddress: mustMACAddress(t, "00:00:5e:00:53:02")},
	}
	tests := []struct {
		name  string
		other infrav1alpha1.TartHost
		want  bool
	}{
		{name: "同じMACは競合", other: infrav1alpha1.TartHost{Name: "host-b", Spec: infrav1alpha1.TartHostSpec{MACAddress: mustMACAddress(t, "00-00-5E-00-53-02")}}, want: true},
		{name: "異なるMACは競合しない", other: infrav1alpha1.TartHost{Name: "host-b", Spec: infrav1alpha1.TartHostSpec{MACAddress: mustMACAddress(t, "00:00:5e:00:53:03")}}, want: false},
		{name: "空のMACは競合しない", other: infrav1alpha1.TartHost{Name: "host-b"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := HasIdentityConflict(base, []infrav1alpha1.TartHost{base, tt.other}); got != tt.want {
				t.Errorf("HasIdentityConflict() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestHasIdentityConflictDetectsDiskIdentityReuse(t *testing.T) {
	t.Parallel()

	base := infrav1alpha1.TartHost{
		Name: "host-a",
		Status: infrav1alpha1.TartHostStatus{Inventory: &infrav1alpha1.HostInventory{
			Disks: []infrav1alpha1.DiskInventory{{DevicePath: "/dev/vda", SizeBytes: 64, Serial: "serial-a", WWID: "wwid-a"}},
		}},
	}
	tests := []struct {
		name  string
		other infrav1alpha1.TartHost
		want  bool
	}{
		{
			name: "same serial",
			other: infrav1alpha1.TartHost{Status: infrav1alpha1.TartHostStatus{Inventory: &infrav1alpha1.HostInventory{
				Disks: []infrav1alpha1.DiskInventory{{DevicePath: "/dev/vdb", SizeBytes: 128, Serial: "serial-a"}},
			}}},
			want: true,
		},
		{
			name: "same wwid with case variation",
			other: infrav1alpha1.TartHost{Status: infrav1alpha1.TartHostStatus{Inventory: &infrav1alpha1.HostInventory{
				Disks: []infrav1alpha1.DiskInventory{{DevicePath: "/dev/vdb", SizeBytes: 128, WWID: "WWID-A"}},
			}}},
			want: true,
		},
		{
			name: "same model and size only",
			other: infrav1alpha1.TartHost{Status: infrav1alpha1.TartHostStatus{Inventory: &infrav1alpha1.HostInventory{
				Disks: []infrav1alpha1.DiskInventory{{DevicePath: "/dev/vdb", SizeBytes: 64, Model: "same-model"}},
			}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := HasIdentityConflict(base, []infrav1alpha1.TartHost{base, tt.other}); got != tt.want {
				t.Fatalf("HasIdentityConflict() = %t, want %t", got, tt.want)
			}
		})
	}

	duplicate := base.DeepCopy()
	duplicate.Status.Inventory.Disks = append(duplicate.Status.Inventory.Disks, infrav1alpha1.DiskInventory{DevicePath: "/dev/vdb", Serial: "serial-a"})
	if !HasIdentityConflictForAny([]infrav1alpha1.TartHost{*duplicate}) {
		t.Fatal("HasIdentityConflictForAny() = false for duplicate disk identity within one Host")
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
