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
	"github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution/model"
	machineexecutionstep "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution/step"
	applicationhealth "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinehealth"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (steps *StepExecutor) observeNodeHealthStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	nodeHealth, err := steps.observeNodeHealth(ctx, machine)
	if err != nil {
		return err
	}
	switch nodeHealth := nodeHealth.(type) {
	case model.NodeHealthUnavailable:
		return nil
	case model.NodeHealthObserved:
		return steps.applyObservedHealthGateStep(ctx, machine, nodeHealth.Observation)
	default:
		return fmt.Errorf("unknown Node health result: %T", nodeHealth)
	}
}

func (steps *StepExecutor) applyObservedHealthGateStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	observation machinehealthdomain.NodeObservation,
) error {
	route, err := steps.planHealthGateRouteStep(ctx, machine, observation)
	if err != nil {
		return err
	}
	patchResult, err := steps.applyHealthGateRouteStep(ctx, machine, route)
	if err != nil {
		return err
	}
	return steps.patchPlannedMachineStatus(ctx, machine, patchResult)
}

func (steps *StepExecutor) planHealthGateRouteStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	observation machinehealthdomain.NodeObservation,
) (model.HealthGateRouteResult, error) {
	state := model.MachineState(machine)
	operationReference, err := steps.resolveOperationReferenceStep(ctx, machine, "health gate")
	if err != nil {
		return nil, err
	}
	switch reference := operationReference.(type) {
	case model.OperationReferenceResolved:
		if !state.Provisioned {
			return model.HealthGateProvisionRoute{Operation: reference.Operation, Observation: observation}, nil
		}
		return machineexecutionstep.DecideProvisionedHealthGateRoute(reference.Operation, observation)
	case model.OperationReferenceAbsent, model.OperationReferenceStale:
		return model.HealthGateNodeStatusRoute{Observation: observation}, nil
	default:
		return nil, fmt.Errorf("unknown Operation reference result for health gate: %T", operationReference)
	}
}

func (steps *StepExecutor) applyHealthGateRouteStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	route model.HealthGateRouteResult,
) (model.MachineStatusPatchResult, error) {
	original := machine.DeepCopy()
	switch route := route.(type) {
	case model.HealthGateNodeStatusRoute:
		machine.Status = applicationhealth.StatusWithNodeHealth(machine, route.Observation)
		return model.MachineStatusPatchRequired{Original: original}, nil
	case model.HealthGateProvisionRoute:
		if err := steps.applyProvisionHealth(ctx, machine, route.Operation, route.Observation); err != nil {
			return nil, err
		}
		return model.MachineStatusPatchRequired{Original: original}, nil
	case model.HealthGateUpdateRoute:
		if err := steps.applyUpdateHealthGate(ctx, machine, route.Operation, route.Observation); err != nil {
			return nil, err
		}
		return model.MachineStatusPatchRequired{Original: original}, nil
	case model.HealthGateUpdateTerminalRoute:
		if err := steps.applyUpdateTerminalStep(ctx, machine, route.Operation, route.Outcome); err != nil {
			return nil, err
		}
		return model.MachineStatusPatchAlreadyApplied{}, nil
	default:
		return nil, fmt.Errorf("unknown Health Gate route result: %T", route)
	}
}

func (steps *StepExecutor) patchPlannedMachineStatus(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	patchResult model.MachineStatusPatchResult,
) error {
	switch patchResult := patchResult.(type) {
	case model.MachineStatusPatchAlreadyApplied:
		return nil
	case model.MachineStatusPatchRequired:
		if err := steps.Status().Patch(ctx, machine, client.MergeFrom(patchResult.Original)); err != nil {
			return fmt.Errorf("set TartMachine status from Node health observation: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown TartMachine status patch result: %T", patchResult)
	}
}

func (steps *StepExecutor) observeNodeHealth(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (model.NodeHealthResult, error) {
	if steps.NodeHealth == nil {
		return model.NodeHealthUnavailable{}, nil
	}
	observation, observed, err := steps.NodeHealth.Observe(ctx, machine)
	if err != nil {
		return nil, fmt.Errorf("observe workload Node health: %w", err)
	}
	if !observed {
		return model.NodeHealthUnavailable{}, nil
	}
	return model.NodeHealthObserved{Observation: observation}, nil
}

func (steps *StepExecutor) applyUpdateHealthGate(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) error {
	decision, err := machineexecutionstep.DecideUpdateHealthGate(operation, observation)
	if err != nil {
		return err
	}
	result, err := steps.applyUpdateHealthGateDecisionStep(ctx, machine, decision)
	if err != nil {
		return err
	}
	switch result.(type) {
	case model.UpdateHealthGateCompleted, model.UpdateHealthGateRollbackStarted:
		return nil
	default:
		return fmt.Errorf("unknown Update health gate effect result: %T", result)
	}
}

func (steps *StepExecutor) applyUpdateHealthGateDecisionStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	decision model.UpdateHealthGateDecisionResult,
) (model.UpdateHealthGateEffectResult, error) {
	switch decision := decision.(type) {
	case model.UpdateHealthGateComplete:
		if err := steps.completeUpdateStep(ctx, machine, decision.Operation); err != nil {
			return nil, err
		}
		return model.UpdateHealthGateCompleted{}, nil
	case model.UpdateHealthGateRollback:
		if err := steps.rollbackUpdateStep(ctx, machine, decision.Operation, decision.Observation); err != nil {
			return nil, err
		}
		return model.UpdateHealthGateRollbackStarted{}, nil
	default:
		return nil, fmt.Errorf("unknown Update health gate decision result: %T", decision)
	}
}
