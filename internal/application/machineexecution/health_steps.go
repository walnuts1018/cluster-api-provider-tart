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
	switch nodeHealth := nodeHealth.(type) {
	case nodeHealthUnavailable:
		return nil
	case nodeHealthObserved:
		return workflow.applyObservedNodeHealth(ctx, machine, nodeHealth.Observation)
	default:
		return fmt.Errorf("unknown Node health result: %T", nodeHealth)
	}
}

func (workflow *Workflow) applyObservedNodeHealth(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	observation machinehealthdomain.NodeObservation,
) error {
	original := machine.DeepCopy()
	patchResult, err := workflow.planObservedNodeHealthStatus(ctx, machine, observation, original)
	if err != nil {
		return err
	}
	return workflow.patchPlannedMachineStatus(ctx, machine, patchResult)
}

func (workflow *Workflow) planObservedNodeHealthStatus(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	observation machinehealthdomain.NodeObservation,
	original *infrastructurev1beta1.TartMachine,
) (machineStatusPatchResult, error) {
	state := machineState(machine)
	operationReference, err := workflow.referencedOperation(ctx, machine, "health gate")
	if err != nil {
		return nil, err
	}
	switch reference := operationReference.(type) {
	case operationReferenceResolved:
		if !state.Provisioned {
			if err := workflow.applyProvisionHealth(ctx, machine, reference.Operation, observation); err != nil {
				return nil, err
			}
			return machineStatusPatchRequired{Original: original}, nil
		} else {
			patchResult, err := workflow.applyUpdateHealth(ctx, machine, reference.Operation, observation, original)
			if err != nil {
				return nil, err
			}
			return patchResult, nil
		}
	case operationReferenceAbsent, operationReferenceStale:
		machine.Status = applicationhealth.StatusWithNodeHealth(machine, observation)
		return machineStatusPatchRequired{Original: original}, nil
	default:
		return nil, fmt.Errorf("unknown Operation reference result for health gate: %T", operationReference)
	}
}

func (workflow *Workflow) patchPlannedMachineStatus(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	patchResult machineStatusPatchResult,
) error {
	switch patchResult := patchResult.(type) {
	case machineStatusPatchAlreadyApplied:
		return nil
	case machineStatusPatchRequired:
		if err := workflow.Status().Patch(ctx, machine, client.MergeFrom(patchResult.Original)); err != nil {
			return fmt.Errorf("set TartMachine status from Node health observation: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown TartMachine status patch result: %T", patchResult)
	}
}

func (workflow *Workflow) observeNodeHealth(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (nodeHealthResult, error) {
	if workflow.NodeHealth == nil {
		return nodeHealthUnavailable{}, nil
	}
	observation, observed, err := workflow.NodeHealth.Observe(ctx, machine)
	if err != nil {
		return nil, fmt.Errorf("observe workload Node health: %w", err)
	}
	if !observed {
		return nodeHealthUnavailable{}, nil
	}
	return nodeHealthObserved{Observation: observation}, nil
}

func (workflow *Workflow) applyUpdateHealth(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
	original *infrastructurev1beta1.TartMachine,
) (machineStatusPatchResult, error) {
	command, err := operationCommand(true, operation)
	if err != nil {
		return nil, fmt.Errorf("decide Update health gate: %w", err)
	}
	switch command := command.(type) {
	case machinelifecycledomain.CommandObserveUpdateHealth:
		return machineStatusPatchRequired{Original: original}, workflow.applyUpdateHealthGate(ctx, machine, operation, observation)
	case machinelifecycledomain.CommandObserveNodeHealth:
		machine.Status = applicationhealth.StatusWithNodeHealth(machine, observation)
	case machinelifecycledomain.CommandApplyUpdateTerminal:
		if err := workflow.applyUpdateTerminalStep(ctx, machine, operation, command.Outcome); err != nil {
			return nil, err
		}
		return machineStatusPatchAlreadyApplied{}, nil
	default:
		return nil, fmt.Errorf("unexpected provisioned TartMachine command: %T", command)
	}
	return machineStatusPatchRequired{Original: original}, nil
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
