package tarthost

import (
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
)

// TestHostInventoryStableSelectorは、複数disk観測時にそれぞれ区別できるstableSelectorが
// previewとして付与され、単独では区別できないdiskは空になることを検証する。この値は
// TartBootstrapConfigのraw patchが使うselectorと同じ規則(WWID→serial→bus path→
// size/rotational)から導出する参考情報であり、ユーザーがhardware identityの生値を照合しなくても
// 「このdiskが他と区別できるか」を確認できるようにする。
func TestHostInventoryStableSelector(t *testing.T) {
	t.Parallel()

	inventory := talos.Inventory{
		Disks: []talos.DiskInventory{
			{DevicePath: "/dev/vda", SizeBytes: 64 * 1024 * 1024 * 1024, Serial: "disk-a", Transport: "virtio"},
			{DevicePath: "/dev/vdb", SizeBytes: 32 * 1024 * 1024 * 1024, Serial: "disk-b", Transport: "virtio"},
			{DevicePath: "/dev/vdc", SizeBytes: 64 * 1024 * 1024 * 1024, Transport: "virtio"},
			{DevicePath: "/dev/vdd", SizeBytes: 64 * 1024 * 1024 * 1024, Transport: "virtio"},
		},
	}

	result := hostInventory(inventory)
	if len(result.Disks) != 4 {
		t.Fatalf("hostInventory() returned %d disks, want 4", len(result.Disks))
	}
	if result.Disks[0].StableSelector == "" {
		t.Fatal("hostInventory() left disk-a's stableSelector empty despite a unique serial")
	}
	if result.Disks[1].StableSelector == "" {
		t.Fatal("hostInventory() left disk-b's stableSelector empty despite a unique serial")
	}
	if result.Disks[0].StableSelector == result.Disks[1].StableSelector {
		t.Fatal("hostInventory() produced identical stableSelector values for distinct disks")
	}
	if result.Disks[2].StableSelector != "" || result.Disks[3].StableSelector != "" {
		t.Fatal("hostInventory() produced a stableSelector for disks that cannot be told apart")
	}
}
