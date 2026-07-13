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
	"context"
	"fmt"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	applicationhealth "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinehealth"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinelifecycle"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (workflow *Workflow) observeNodeHealthStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	nodeHealth, err := workflow.observeNodeHealth(ctx, machine)
	if err != nil {
		return err
	}
	if !nodeHealth.Observed {
		return nil
	}

	original := machine.DeepCopy()
	state := machineState(machine)
	operation, hasOperation, err := workflow.referencedOperation(ctx, machine, "health gate")
	if err != nil {
		return err
	}
	if !state.Provisioned && hasOperation {
		if err := workflow.applyProvisionHealth(ctx, machine, operation, nodeHealth.Observation); err != nil {
			return err
		}
	} else if state.Provisioned && hasOperation {
		terminalHandled, err := workflow.applyUpdateHealth(ctx, machine, operation, nodeHealth.Observation)
		if err != nil {
			return err
		}
		if terminalHandled {
			return nil
		}
	} else {
		machine.Status = applicationhealth.StatusWithNodeHealth(machine, nodeHealth.Observation)
	}

	if err := workflow.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("set TartMachine Node health condition: %w", err)
	}
	return nil
}

func (workflow *Workflow) observeNodeHealth(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (nodeHealthObservation, error) {
	if workflow.NodeHealth == nil {
		return nodeHealthObservation{}, nil
	}
	observation, observed, err := workflow.NodeHealth.Observe(ctx, machine)
	if err != nil {
		return nodeHealthObservation{}, fmt.Errorf("observe workload Node health: %w", err)
	}
	return nodeHealthObservation{Observation: observation, Observed: observed}, nil
}

func (workflow *Workflow) applyUpdateHealth(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) (bool, error) {
	command, err := operationCommand(true, operation)
	if err != nil {
		return false, fmt.Errorf("decide Update health gate: %w", err)
	}
	switch command := command.(type) {
	case machinelifecycledomain.CommandObserveUpdateHealth:
		return false, workflow.applyUpdateHealthGate(ctx, machine, operation, observation)
	case machinelifecycledomain.CommandObserveNodeHealth:
		machine.Status = applicationhealth.StatusWithNodeHealth(machine, observation)
	case machinelifecycledomain.CommandApplyUpdateTerminal:
		if err := workflow.applyUpdateTerminalStep(ctx, machine, operation, command.Outcome); err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, fmt.Errorf("unexpected provisioned TartMachine command: %T", command)
	}
	return false, nil
}

func (workflow *Workflow) applyUpdateHealthGate(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) error {
	healthCommand := machinelifecycledomain.DecideUpdateHealth(machinehealthdomain.EvaluateNode(observation))
	switch healthCommand.(type) {
	case machinelifecycledomain.CommandCompleteUpdate:
		return workflow.completeUpdateStep(ctx, machine, operation)
	case machinelifecycledomain.CommandRollbackUpdate:
		return workflow.rollbackUpdateStep(ctx, machine, operation, observation)
	default:
		return fmt.Errorf("unknown Update health command: %T", healthCommand)
	}
}
