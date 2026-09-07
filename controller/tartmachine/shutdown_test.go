package tartmachine

import (
	"testing"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
)

func TestShutdownConfirmed(t *testing.T) {
	t.Parallel()
	hostID := "123e4567-e89b-12d3-a456-426614174000"
	machineUID := types.UID("machine-uid")
	machine := &infrav1alpha1.TartMachine{}
	machine.UID = machineUID

	host := &infrav1alpha1.TartHost{}
	host.Spec.HostID = hostID
	host.Status.Inventory = &infrav1alpha1.HostInventory{BootID: "boot-123"}

	tests := map[string]struct {
		confirmation *infrav1alpha1.ShutdownConfirmation
		inventoryBootID string
		want           bool
	}{
		"valid with BootID": {
			confirmation: &infrav1alpha1.ShutdownConfirmation{ConsumerUID: machineUID, HostID: hostID, BootID: "boot-123"},
			want: true,
		},
		"BootID mismatch": {
			confirmation: &infrav1alpha1.ShutdownConfirmation{ConsumerUID: machineUID, HostID: hostID, BootID: "boot-999"},
			want: false,
		},
		"inventory BootID present but confirmation BootID empty": {
			confirmation: &infrav1alpha1.ShutdownConfirmation{ConsumerUID: machineUID, HostID: hostID, BootID: ""},
			want: false,
		},
		"consumerUID mismatch": {
			confirmation: &infrav1alpha1.ShutdownConfirmation{ConsumerUID: "other-uid", HostID: hostID, BootID: "boot-123"},
			want: false,
		},
		"hostID mismatch": {
			confirmation: &infrav1alpha1.ShutdownConfirmation{ConsumerUID: machineUID, HostID: "other-host-id", BootID: "boot-123"},
			want: false,
		},
		"nil confirmation": {
			confirmation: nil,
			want: false,
		},
		"empty BootID allowed when inventory BootID empty": {
			confirmation: &infrav1alpha1.ShutdownConfirmation{ConsumerUID: machineUID, HostID: hostID, BootID: ""},
			inventoryBootID: "",
			want: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if tt.inventoryBootID != "" {
				host.Status.Inventory.BootID = tt.inventoryBootID
			} else if name == "empty BootID allowed when inventory BootID empty" {
				host.Status.Inventory.BootID = ""
			} else {
				host.Status.Inventory.BootID = "boot-123"
			}
			host.Spec.ShutdownConfirmation = tt.confirmation
			if got := shutdownConfirmed(host, machine); got != tt.want {
				t.Fatalf("shutdownConfirmed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsShutdownConfirmationRequired(t *testing.T) {
	t.Parallel()
	if !isShutdownConfirmationRequired(infrav1alpha1.PowerBackendWakeOnLAN) {
		t.Fatal("WakeOnLAN should require confirmation")
	}
	if !isShutdownConfirmationRequired(infrav1alpha1.PowerBackendManual) {
		t.Fatal("Manual should require confirmation")
	}
	if isShutdownConfirmationRequired(infrav1alpha1.PowerBackendRedfish) {
		t.Fatal("Redfish should not require confirmation")
	}
}
