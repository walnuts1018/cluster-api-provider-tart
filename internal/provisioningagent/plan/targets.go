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
	case agentprotocol.OperationTypeClean, agentprotocol.OperationTypeWipeAll:
		return nil
	default:
		return fmt.Errorf("%w: unsupported operation type %q", ErrUnsafeTarget, value.OperationType)
	}
}

func updateRoles(activeSlot string) map[agentprotocol.DiskRole]struct{} {
	switch activeSlot {
	case "A":
		//nolint:exhaustive // 更新対象だけを返す。State/Dataやactive slotを含めると安全判定が壊れる。
		return map[agentprotocol.DiskRole]struct{}{
			agentprotocol.DiskRoleBoot:    {},
			agentprotocol.DiskRoleOSB:     {},
			agentprotocol.DiskRoleVerityB: {},
		}
	case "B":
		//nolint:exhaustive // 更新対象だけを返す。State/Dataやactive slotを含めると安全判定が壊れる。
		return map[agentprotocol.DiskRole]struct{}{
			agentprotocol.DiskRoleBoot:    {},
			agentprotocol.DiskRoleOSA:     {},
			agentprotocol.DiskRoleVerityA: {},
		}
	default:
		return nil
	}
}
