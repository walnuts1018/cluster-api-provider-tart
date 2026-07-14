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
	machineexecutionmodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution/model"
	machineexecutionstep "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution/step"
	applicationhealth "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinehealth"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
)

func (steps *StepExecutor) EnsureProvisionReference(
	ctx context.Context,
	provisioning machineexecutionmodel.ProvisioningMachine,
) (machineexecutionmodel.ProvisionReferenceResult, error) {
	return steps.ensureProvisionReferenceStep(ctx, provisioning)
}

func (steps *StepExecutor) StartProvision(
	ctx context.Context,
	provisioning machineexecutionmodel.ProvisioningMachine,
) error {
	return steps.startProvisionStep(ctx, provisioning)
}

func (steps *StepExecutor) ResolveProvisionProgressReference(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (machineexecutionmodel.ProvisionProgressReferenceResult, error) {
	return steps.resolveProvisionProgressReferenceStep(ctx, machine)
}

func (steps *StepExecutor) ClearStaleProvisionOperationReference(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	reference *infrastructurev1beta1.ResourceReference,
) error {
	_, err := steps.clearStaleProvisionOperationReferenceStep(ctx, machine, reference)
	return err
}

func (steps *StepExecutor) DecideProvisionProgress(
	operation *infrastructurev1beta1.TartHostOperation,
) (machineexecutionmodel.ProvisionProgressDecisionResult, error) {
	return machineexecutionstep.DecideProvisionProgress(operation)
}

func (steps *StepExecutor) PatchProvisionFailureStatus(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	failure machineexecutionmodel.ProvisionProgressFailed,
) error {
	_, err := steps.patchProvisionFailureStatusStep(ctx, machine, failure)
	return err
}

func (steps *StepExecutor) DecideUpdateOperation(
	ctx context.Context,
	provisioned machineexecutionmodel.ProvisionedMachine,
) (machineexecutionmodel.UpdateOperationDecisionResult, error) {
	return steps.decideUpdateOperationStep(ctx, provisioned)
}

func (steps *StepExecutor) ApplyUpdateTerminal(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	decision machineexecutionmodel.UpdateOperationApplyTerminal,
) error {
	return steps.applyUpdateTerminalStep(ctx, machine, decision.Operation, decision.Outcome)
}

func (steps *StepExecutor) ObserveNodeHealth(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (machineexecutionmodel.NodeHealthResult, error) {
	return steps.observeNodeHealth(ctx, machine)
}

func (steps *StepExecutor) PlanHealthGateRoute(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	observation machinehealthdomain.NodeObservation,
) (machineexecutionmodel.HealthGateRouteResult, error) {
	return steps.planHealthGateRouteStep(ctx, machine, observation)
}

func (steps *StepExecutor) ApplyNodeHealthStatus(
	machine *infrastructurev1beta1.TartMachine,
	observation machinehealthdomain.NodeObservation,
) (machineexecutionmodel.MachineStatusPatchResult, error) {
	original := machine.DeepCopy()
	machine.Status = applicationhealth.StatusWithNodeHealth(machine, observation)
	return machineexecutionmodel.MachineStatusPatchRequired{Original: original}, nil
}

func (steps *StepExecutor) DecideProvisionHealthGate(
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) (machineexecutionmodel.ProvisionHealthGateDecisionResult, error) {
	return machineexecutionstep.DecideProvisionHealthGate(operation, observation)
}

func (steps *StepExecutor) CompleteProvision(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) error {
	return steps.completeProvisionStep(ctx, machine, operation, observation)
}

func (steps *StepExecutor) SetProvisionHealthPending(
	machine *infrastructurev1beta1.TartMachine,
	pending machineexecutionmodel.ProvisionHealthGatePending,
) (machineexecutionmodel.MachineStatusPatchResult, error) {
	original := machine.DeepCopy()
	steps.setProvisionHealthPendingStep(machine, pending)
	return machineexecutionmodel.MachineStatusPatchRequired{Original: original}, nil
}

func (steps *StepExecutor) DecideUpdateHealthGate(
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) (machineexecutionmodel.UpdateHealthGateDecisionResult, error) {
	return machineexecutionstep.DecideUpdateHealthGate(operation, observation)
}

func (steps *StepExecutor) CompleteUpdate(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	return steps.completeUpdateStep(ctx, machine, operation)
}

func (steps *StepExecutor) RollbackUpdate(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) error {
	return steps.rollbackUpdateStep(ctx, machine, operation, observation)
}

func (steps *StepExecutor) PatchPlannedMachineStatus(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	patchResult machineexecutionmodel.MachineStatusPatchResult,
) error {
	if patchResult == nil {
		return fmt.Errorf("patch planned TartMachine status: patch result is nil")
	}
	return steps.patchPlannedMachineStatus(ctx, machine, patchResult)
}
