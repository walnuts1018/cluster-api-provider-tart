package provisioningagent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/disk"
	agentplan "github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/plan"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

type recordingWriter struct {
	calls int
}

func (writer *recordingWriter) WriteTargets(context.Context, agentprotocol.ValidatedPlan, disk.Device) error {
	writer.calls++
	return nil
}

func TestServiceDoesNotWriteWhenDiskSelectionFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		devices []disk.Device
	}{
		{name: "候補0台"},
		{name: "候補2台", devices: []disk.Device{matchingDisk("/dev/sda"), matchingDisk("/dev/sdb")}},
		{name: "identity不一致", devices: []disk.Device{{
			Path:         "/dev/sda",
			ByIDPaths:    []string{"/dev/disk/by-id/other"},
			SerialNumber: "serial-root",
			SizeBytes:    100,
		}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			writer := &recordingWriter{}
			service := NewService(writer)
			err := service.Execute(t.Context(), validatedPlan(t, agentprotocol.OperationTypeProvision, "", []agentprotocol.DiskRole{agentprotocol.DiskRoleOSA}), tt.devices)
			if err == nil {
				t.Fatal("Execute() accepted unsafe disk selection")
			}
			if writer.calls != 0 {
				t.Fatalf("WriteTargets() calls = %d, want 0", writer.calls)
			}
		})
	}
}

func TestServiceDoesNotWriteUnsafeUpdateTarget(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{}
	service := NewService(writer)
	err := service.Execute(
		t.Context(),
		validatedPlan(t, agentprotocol.OperationTypeUpdate, "A", []agentprotocol.DiskRole{agentprotocol.DiskRoleOSA}),
		[]disk.Device{matchingDisk("/dev/sda")},
	)
	if !errors.Is(err, agentplan.ErrUnsafeTarget) {
		t.Fatalf("Execute() error = %v, want ErrUnsafeTarget", err)
	}
	if writer.calls != 0 {
		t.Fatalf("WriteTargets() calls = %d, want 0", writer.calls)
	}
}

func TestServiceWritesAfterEverySafetyCheckPasses(t *testing.T) {
	t.Parallel()

	writer := &recordingWriter{}
	service := NewService(writer)
	if err := service.Execute(
		t.Context(),
		validatedPlan(t, agentprotocol.OperationTypeUpdate, "A", []agentprotocol.DiskRole{agentprotocol.DiskRoleOSB}),
		[]disk.Device{matchingDisk("/dev/sda")},
	); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if writer.calls != 1 {
		t.Fatalf("WriteTargets() calls = %d, want 1", writer.calls)
	}
}

func validatedPlan(
	t *testing.T,
	operation agentprotocol.OperationType,
	activeSlot string,
	roles []agentprotocol.DiskRole,
) agentprotocol.ValidatedPlan {
	t.Helper()
	plan, err := agentprotocol.ValidatePlan(agentprotocol.Plan{
		APIVersion:    agentprotocol.APIVersion,
		OperationUID:  "operation-1",
		HostUID:       "host-1",
		OperationType: operation,
		ActiveSlot:    activeSlot,
		Deadline:      time.Unix(1, 0).UTC(),
		RootDevice: agentprotocol.RootDevice{
			DeviceName:   "/dev/disk/by-id/wwn-root",
			SerialNumber: "serial-root",
			MinSizeBytes: 100,
		},
		Artifact: agentprotocol.Artifact{
			Ref:            "oci://registry.test/os@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ManifestDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Generation:     1,
		},
		AllowedTargetRoles: roles,
		Steps:              []agentprotocol.PlanStep{{Name: "Write"}},
	})
	if err != nil {
		t.Fatalf("ValidatePlan() error = %v", err)
	}
	return plan
}

func matchingDisk(path string) disk.Device {
	return disk.Device{
		Path:         path,
		ByIDPaths:    []string{"/dev/disk/by-id/wwn-root"},
		SerialNumber: "serial-root",
		SizeBytes:    100,
	}
}
