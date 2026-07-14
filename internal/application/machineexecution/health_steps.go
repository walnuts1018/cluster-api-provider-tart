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
	machineexecutionstep "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution/step"
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
		return workflow.applyObservedHealthGateStep(ctx, machine, nodeHealth.Observation)
	default:
		return fmt.Errorf("unknown Node health result: %T", nodeHealth)
	}
}

func (workflow *Workflow) applyObservedHealthGateStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	observation machinehealthdomain.NodeObservation,
) error {
	route, err := workflow.planHealthGateRouteStep(ctx, machine, observation)
	if err != nil {
		return err
	}
	patchResult, err := workflow.applyHealthGateRouteStep(ctx, machine, route)
	if err != nil {
		return err
	}
	return workflow.patchPlannedMachineStatus(ctx, machine, patchResult)
}

func (workflow *Workflow) planHealthGateRouteStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	observation machinehealthdomain.NodeObservation,
) (healthGateRouteResult, error) {
	state := machineState(machine)
	operationReference, err := workflow.resolveOperationReferenceStep(ctx, machine, "health gate")
	if err != nil {
		return nil, err
	}
	switch reference := operationReference.(type) {
	case operationReferenceResolved:
		if !state.Provisioned {
			return healthGateProvisionRoute{Operation: reference.Operation, Observation: observation}, nil
		}
		return decideProvisionedHealthGateRouteStep(reference.Operation, observation)
	case operationReferenceAbsent, operationReferenceStale:
		return healthGateNodeStatusRoute{Observation: observation}, nil
	default:
		return nil, fmt.Errorf("unknown Operation reference result for health gate: %T", operationReference)
	}
}

func decideProvisionedHealthGateRouteStep(
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) (healthGateRouteResult, error) {
	route, err := machineexecutionstep.DecideOperationRoute(machineexecutionstep.OperationProvisioned{}, operation)
	if err != nil {
		return nil, fmt.Errorf("decide Update health gate: %w", err)
	}
	switch route := route.(type) {
	case machineexecutionstep.OperationUpdateHealthRoute:
		return healthGateUpdateRoute{Operation: route.Operation, Observation: observation}, nil
	case machineexecutionstep.OperationNodeHealthRoute:
		return healthGateNodeStatusRoute{Observation: observation}, nil
	case machineexecutionstep.OperationUpdateTerminalRoute:
		return healthGateUpdateTerminalRoute{Operation: route.Operation, Outcome: route.Outcome}, nil
	default:
		return nil, fmt.Errorf("unknown provisioned TartMachine route: %T", route)
	}
}

func (workflow *Workflow) applyHealthGateRouteStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	route healthGateRouteResult,
) (machineStatusPatchResult, error) {
	original := machine.DeepCopy()
	switch route := route.(type) {
	case healthGateNodeStatusRoute:
		machine.Status = applicationhealth.StatusWithNodeHealth(machine, route.Observation)
		return machineStatusPatchRequired{Original: original}, nil
	case healthGateProvisionRoute:
		if err := workflow.applyProvisionHealth(ctx, machine, route.Operation, route.Observation); err != nil {
			return nil, err
		}
		return machineStatusPatchRequired{Original: original}, nil
	case healthGateUpdateRoute:
		if err := workflow.applyUpdateHealthGate(ctx, machine, route.Operation, route.Observation); err != nil {
			return nil, err
		}
		return machineStatusPatchRequired{Original: original}, nil
	case healthGateUpdateTerminalRoute:
		if err := workflow.applyUpdateTerminalStep(ctx, machine, route.Operation, route.Outcome); err != nil {
			return nil, err
		}
		return machineStatusPatchAlreadyApplied{}, nil
	default:
		return nil, fmt.Errorf("unknown Health Gate route result: %T", route)
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

func (workflow *Workflow) applyUpdateHealthGate(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) error {
	decision, err := decideUpdateHealthGateStep(operation, observation)
	if err != nil {
		return err
	}
	result, err := workflow.applyUpdateHealthGateDecisionStep(ctx, machine, decision)
	if err != nil {
		return err
	}
	switch result.(type) {
	case updateHealthGateCompleted, updateHealthGateRollbackStarted:
		return nil
	default:
		return fmt.Errorf("unknown Update health gate effect result: %T", result)
	}
}

func decideUpdateHealthGateStep(
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) (updateHealthGateDecisionResult, error) {
	healthCommand := machinelifecycledomain.DecideUpdateHealth(machinehealthdomain.EvaluateNode(observation))
	switch healthCommand.(type) {
	case machinelifecycledomain.CommandCompleteUpdate:
		return updateHealthGateComplete{Operation: operation}, nil
	case machinelifecycledomain.CommandRollbackUpdate:
		return updateHealthGateRollback{Operation: operation, Observation: observation}, nil
	default:
		return nil, fmt.Errorf("unknown Update health command: %T", healthCommand)
	}
}

func (workflow *Workflow) applyUpdateHealthGateDecisionStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	decision updateHealthGateDecisionResult,
) (updateHealthGateEffectResult, error) {
	switch decision := decision.(type) {
	case updateHealthGateComplete:
		if err := workflow.completeUpdateStep(ctx, machine, decision.Operation); err != nil {
			return nil, err
		}
		return updateHealthGateCompleted{}, nil
	case updateHealthGateRollback:
		if err := workflow.rollbackUpdateStep(ctx, machine, decision.Operation, decision.Observation); err != nil {
			return nil, err
		}
		return updateHealthGateRollbackStarted{}, nil
	default:
		return nil, fmt.Errorf("unknown Update health gate decision result: %T", decision)
	}
}
