package plan

import (
	"errors"
	"testing"
	"time"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

func TestValidateTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		operation  agentprotocol.OperationType
		activeSlot string
		roles      []agentprotocol.DiskRole
		wantErr    error
	}{
		{name: "初期Provisionは全roleを許可する", operation: agentprotocol.OperationTypeProvision, roles: []agentprotocol.DiskRole{
			agentprotocol.DiskRoleBoot, agentprotocol.DiskRoleOSA, agentprotocol.DiskRoleVerityA,
			agentprotocol.DiskRoleState, agentprotocol.DiskRoleData,
		}},
		{name: "UpdateはInactive SlotとBootだけを許可する", operation: agentprotocol.OperationTypeUpdate, activeSlot: "A", roles: []agentprotocol.DiskRole{
			agentprotocol.DiskRoleBoot, agentprotocol.DiskRoleOSB, agentprotocol.DiskRoleVerityB,
		}},
		{name: "UpdateのActive OSを拒否する", operation: agentprotocol.OperationTypeUpdate, activeSlot: "A", roles: []agentprotocol.DiskRole{agentprotocol.DiskRoleOSA}, wantErr: ErrUnsafeTarget},
		{name: "UpdateのActive Verityを拒否する", operation: agentprotocol.OperationTypeUpdate, activeSlot: "B", roles: []agentprotocol.DiskRole{agentprotocol.DiskRoleVerityB}, wantErr: ErrUnsafeTarget},
		{name: "UpdateのStateを拒否する", operation: agentprotocol.OperationTypeUpdate, activeSlot: "A", roles: []agentprotocol.DiskRole{agentprotocol.DiskRoleState}, wantErr: ErrUnsafeTarget},
		{name: "UpdateのDataを拒否する", operation: agentprotocol.OperationTypeUpdate, activeSlot: "A", roles: []agentprotocol.DiskRole{agentprotocol.DiskRoleData}, wantErr: ErrUnsafeTarget},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			validated, err := agentprotocol.ValidatePlan(validPlan(tt.operation, tt.activeSlot, tt.roles))
			if err != nil {
				t.Fatalf("ValidatePlan() error = %v", err)
			}
			if err := ValidateTargets(validated); !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateTargets() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func validPlan(operation agentprotocol.OperationType, activeSlot string, roles []agentprotocol.DiskRole) agentprotocol.Plan {
	return agentprotocol.Plan{
		APIVersion:    agentprotocol.APIVersion,
		OperationUID:  "operation-1",
		HostUID:       "host-1",
		OperationType: operation,
		ActiveSlot:    activeSlot,
		Deadline:      time.Unix(1, 0).UTC(),
		RootDevice: agentprotocol.RootDevice{
			DeviceName:   "/dev/disk/by-id/wwn-root",
			SerialNumber: "serial-root",
			MinSizeBytes: 1,
		},
		Artifact: agentprotocol.Artifact{
			Ref:            "oci://registry.test/repository@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ManifestDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Generation:     1,
		},
		AllowedTargetRoles: roles,
		Steps:              []agentprotocol.PlanStep{{Name: "Write"}},
	}
}
