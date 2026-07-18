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

package step

import (
	"fmt"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/entity/machinelifecycle"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/operation"
)

type OperationRoute interface {
	isOperationRoute()
}

type OperationMachineState interface {
	isOperationMachineState()
}

type OperationProvisioning struct{}

type OperationProvisioned struct{}

func (OperationProvisioning) isOperationMachineState() {}
func (OperationProvisioned) isOperationMachineState()  {}

type OperationProvisionHealthRoute struct {
	Operation *infrastructurev1beta1.TartHostOperation
}

type OperationProvisionFailedRoute struct {
	Operation *infrastructurev1beta1.TartHostOperation
	Reason    string
}

type OperationUpdateHealthRoute struct {
	Operation *infrastructurev1beta1.TartHostOperation
}

type OperationNodeHealthRoute struct{}

type OperationUpdateTerminalRoute struct {
	Operation *infrastructurev1beta1.TartHostOperation
	Outcome   machinelifecycledomain.UpdateOutcome
}

func (OperationProvisionHealthRoute) isOperationRoute() {}
func (OperationProvisionFailedRoute) isOperationRoute() {}
func (OperationUpdateHealthRoute) isOperationRoute()    {}
func (OperationNodeHealthRoute) isOperationRoute()      {}
func (OperationUpdateTerminalRoute) isOperationRoute()  {}

func DecideOperationRoute(
	state OperationMachineState,
	operation *infrastructurev1beta1.TartHostOperation,
) (OperationRoute, error) {
	command, err := operationCommand(state, operation)
	if err != nil {
		return nil, err
	}
	switch command := command.(type) {
	case machinelifecycledomain.CommandMarkProvisionFailed:
		return OperationProvisionFailedRoute{Operation: operation, Reason: command.Reason}, nil
	case machinelifecycledomain.CommandObserveProvisionHealth:
		return OperationProvisionHealthRoute{Operation: operation}, nil
	case machinelifecycledomain.CommandObserveUpdateHealth:
		return OperationUpdateHealthRoute{Operation: operation}, nil
	case machinelifecycledomain.CommandObserveNodeHealth:
		return OperationNodeHealthRoute{}, nil
	case machinelifecycledomain.CommandApplyUpdateTerminal:
		return OperationUpdateTerminalRoute{Operation: operation, Outcome: command.Outcome}, nil
	default:
		return nil, fmt.Errorf("unexpected TartMachine operation command: %T", command)
	}
}

func operationCommand(
	state OperationMachineState,
	operation *infrastructurev1beta1.TartHostOperation,
) (machinelifecycledomain.OperationCommand, error) {
	operationState, err := operationState(operation)
	if err != nil {
		return nil, err
	}
	switch state.(type) {
	case OperationProvisioning:
		return machinelifecycledomain.DecideProvisioningOperation(operationState)
	case OperationProvisioned:
		return machinelifecycledomain.DecideProvisionedOperation(operationState)
	default:
		return nil, fmt.Errorf("unknown TartMachine operation state: %T", state)
	}
}

func operationState(
	operation *infrastructurev1beta1.TartHostOperation,
) (machinelifecycledomain.OperationState, error) {
	kind, err := operationdomain.ParseKind(string(operation.Spec.Type))
	if err != nil {
		return machinelifecycledomain.OperationState{}, err
	}
	var phase operationdomain.Phase
	if operation.Status.Phase != "" {
		phase, err = operationdomain.ParsePhase(string(operation.Status.Phase))
		if err != nil {
			return machinelifecycledomain.OperationState{}, err
		}
	}
	return machinelifecycledomain.OperationState{Kind: kind, Phase: phase}, nil
}
