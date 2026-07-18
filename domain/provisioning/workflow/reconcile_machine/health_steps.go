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
	machineexecutionstep "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/step/machineexecution"
	model "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/step/machineexecution/model"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/machinehealth"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (steps *workflowRuntime) planHealthGateRouteStep(
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

func (steps *workflowRuntime) patchPlannedMachineStatus(
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

func (steps *workflowRuntime) observeNodeHealth(
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
