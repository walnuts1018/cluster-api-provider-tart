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
	machineexecutionmodel "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/step/machineexecution/model"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/machinehealth"
)

func (steps *workflowRuntime) PlanHealthGateRoute(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	observation machinehealthdomain.NodeObservation,
) (machineexecutionmodel.HealthGateRouteResult, error) {
	return steps.planHealthGateRouteStep(ctx, machine, observation)
}

func (steps *workflowRuntime) ApplyNodeHealthStatus(
	machine *infrastructurev1beta1.TartMachine,
	observation machinehealthdomain.NodeObservation,
) (machineexecutionmodel.MachineStatusPatchResult, error) {
	original := machine.DeepCopy()
	machine.Status = machineexecutionstep.StatusWithNodeHealth(machine, observation)
	return machineexecutionmodel.MachineStatusPatchRequired{Original: original}, nil
}

func (steps *workflowRuntime) DecideProvisionHealthGate(
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) (machineexecutionmodel.ProvisionHealthGateDecisionResult, error) {
	return machineexecutionstep.DecideProvisionHealthGate(operation, observation)
}

func (steps *workflowRuntime) CompleteProvision(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) error {
	return steps.completeProvisionStep(ctx, machine, operation, observation)
}

func (steps *workflowRuntime) SetProvisionHealthPending(
	machine *infrastructurev1beta1.TartMachine,
	pending machineexecutionmodel.ProvisionHealthGatePending,
) (machineexecutionmodel.MachineStatusPatchResult, error) {
	original := machine.DeepCopy()
	steps.setProvisionHealthPendingStep(machine, pending)
	return machineexecutionmodel.MachineStatusPatchRequired{Original: original}, nil
}

func (steps *workflowRuntime) DecideUpdateHealthGate(
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) (machineexecutionmodel.UpdateHealthGateDecisionResult, error) {
	return machineexecutionstep.DecideUpdateHealthGate(operation, observation)
}

func (steps *workflowRuntime) CompleteUpdate(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	return steps.completeUpdateStep(ctx, machine, operation)
}

func (steps *workflowRuntime) RollbackUpdate(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) error {
	return steps.rollbackUpdateStep(ctx, machine, operation, observation)
}

func (steps *workflowRuntime) PatchPlannedMachineStatus(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	patchResult machineexecutionmodel.MachineStatusPatchResult,
) error {
	if patchResult == nil {
		return fmt.Errorf("patch planned TartMachine status: patch result is nil")
	}
	return steps.patchPlannedMachineStatus(ctx, machine, patchResult)
}
