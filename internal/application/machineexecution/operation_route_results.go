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

package machineexecution

import (
	"fmt"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinelifecycle"
)

type machineOperationRouteResult interface {
	isMachineOperationRouteResult()
}

type machineOperationState interface {
	isMachineOperationState()
}

type machineOperationProvisioning struct{}

type machineOperationProvisioned struct{}

func (machineOperationProvisioning) isMachineOperationState() {}
func (machineOperationProvisioned) isMachineOperationState()  {}

type machineOperationProvisionHealthRoute struct {
	Operation *infrastructurev1beta1.TartHostOperation
}

type machineOperationProvisionFailedRoute struct {
	Operation *infrastructurev1beta1.TartHostOperation
	Reason    string
}

type machineOperationUpdateHealthRoute struct {
	Operation *infrastructurev1beta1.TartHostOperation
}

type machineOperationNodeHealthRoute struct{}

type machineOperationUpdateTerminalRoute struct {
	Operation *infrastructurev1beta1.TartHostOperation
	Outcome   machinelifecycledomain.UpdateOutcome
}

func (machineOperationProvisionHealthRoute) isMachineOperationRouteResult() {}
func (machineOperationProvisionFailedRoute) isMachineOperationRouteResult() {}
func (machineOperationUpdateHealthRoute) isMachineOperationRouteResult()    {}
func (machineOperationNodeHealthRoute) isMachineOperationRouteResult()      {}
func (machineOperationUpdateTerminalRoute) isMachineOperationRouteResult()  {}

func decideMachineOperationRouteStep(
	state machineOperationState,
	operation *infrastructurev1beta1.TartHostOperation,
) (machineOperationRouteResult, error) {
	provisioned, err := machineOperationProvisionedFlag(state)
	if err != nil {
		return nil, err
	}
	command, err := operationCommand(provisioned, operation)
	if err != nil {
		return nil, err
	}
	switch command := command.(type) {
	case machinelifecycledomain.CommandMarkProvisionFailed:
		return machineOperationProvisionFailedRoute{Operation: operation, Reason: command.Reason}, nil
	case machinelifecycledomain.CommandObserveProvisionHealth:
		return machineOperationProvisionHealthRoute{Operation: operation}, nil
	case machinelifecycledomain.CommandObserveUpdateHealth:
		return machineOperationUpdateHealthRoute{Operation: operation}, nil
	case machinelifecycledomain.CommandObserveNodeHealth:
		return machineOperationNodeHealthRoute{}, nil
	case machinelifecycledomain.CommandApplyUpdateTerminal:
		return machineOperationUpdateTerminalRoute{Operation: operation, Outcome: command.Outcome}, nil
	default:
		return nil, fmt.Errorf("unexpected TartMachine operation command: %T", command)
	}
}

func machineOperationProvisionedFlag(state machineOperationState) (bool, error) {
	switch state.(type) {
	case machineOperationProvisioning:
		return false, nil
	case machineOperationProvisioned:
		return true, nil
	default:
		return false, fmt.Errorf("unknown TartMachine operation state: %T", state)
	}
}
