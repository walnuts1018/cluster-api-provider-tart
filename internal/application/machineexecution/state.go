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
	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinelifecycle"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

type activeMachine struct {
	Machine *infrastructurev1beta1.TartMachine
	State   machinelifecycledomain.MachineState
}

type provisioningMachine struct {
	Machine *infrastructurev1beta1.TartMachine
	State   machinelifecycledomain.MachineState
}

type provisionedMachine struct {
	Machine *infrastructurev1beta1.TartMachine
	State   machinelifecycledomain.MachineState
}

type nodeHealthObservation struct {
	Observation machinehealthdomain.NodeObservation
	Observed    bool
}

func machineState(machine *infrastructurev1beta1.TartMachine) machinelifecycledomain.MachineState {
	provisioned := machine.Status.Initialization.Provisioned != nil && *machine.Status.Initialization.Provisioned
	return machinelifecycledomain.MachineState{
		Provisioned:  provisioned,
		HasOperation: machine.Status.OperationRef != nil,
	}
}

func operationCommand(
	provisioned bool,
	operation *infrastructurev1beta1.TartHostOperation,
) (machinelifecycledomain.OperationCommand, error) {
	kind, err := operationdomain.ParseKind(string(operation.Spec.Type))
	if err != nil {
		return nil, err
	}
	var phase operationdomain.Phase
	if operation.Status.Phase != "" {
		phase, err = operationdomain.ParsePhase(string(operation.Status.Phase))
		if err != nil {
			return nil, err
		}
	}
	return machinelifecycledomain.DecideOperation(
		machinelifecycledomain.MachineState{Provisioned: provisioned, HasOperation: true},
		machinelifecycledomain.OperationState{Kind: kind, Phase: phase},
	)
}
