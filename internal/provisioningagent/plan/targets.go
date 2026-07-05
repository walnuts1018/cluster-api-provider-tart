package plan

import (
	"errors"
	"fmt"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

var ErrUnsafeTarget = errors.New("UnsafePlanTarget")

// ValidateTargetsはUpdateがActive Slot、State、Dataへ書けないことをAgent境界でも検証する。
func ValidateTargets(plan agentprotocol.ValidatedPlan) error {
	value := plan.Value()
	switch value.OperationType {
	case agentprotocol.OperationTypeProvision:
		return nil
	case agentprotocol.OperationTypeUpdate:
		allowed := updateRoles(value.ActiveSlot)
		for _, role := range value.AllowedTargetRoles {
			if _, ok := allowed[role]; !ok {
				return fmt.Errorf("%w: role %q is not writable while slot %s is active", ErrUnsafeTarget, role, value.ActiveSlot)
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: unsupported operation type %q", ErrUnsafeTarget, value.OperationType)
	}
}

func updateRoles(activeSlot string) map[agentprotocol.DiskRole]struct{} {
	switch activeSlot {
	case "A":
		return map[agentprotocol.DiskRole]struct{}{
			agentprotocol.DiskRoleBoot:    {},
			agentprotocol.DiskRoleOSB:     {},
			agentprotocol.DiskRoleVerityB: {},
		}
	case "B":
		return map[agentprotocol.DiskRole]struct{}{
			agentprotocol.DiskRoleBoot:    {},
			agentprotocol.DiskRoleOSA:     {},
			agentprotocol.DiskRoleVerityA: {},
		}
	default:
		return nil
	}
}
