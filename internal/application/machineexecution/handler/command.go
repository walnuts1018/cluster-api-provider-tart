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

package handler

import (
	"context"
	"fmt"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machineexecutionmodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution/model"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinelifecycle"
)

type Steps interface {
	EnsureProvisionReference(
		context.Context,
		machineexecutionmodel.ProvisioningMachine,
	) (machineexecutionmodel.ProvisionReferenceResult, error)
	StartProvision(context.Context, machineexecutionmodel.ProvisioningMachine) error
	ResolveProvisionProgressReference(
		context.Context,
		*infrastructurev1beta1.TartMachine,
	) (machineexecutionmodel.ProvisionProgressReferenceResult, error)
	ClearStaleProvisionOperationReference(
		context.Context,
		*infrastructurev1beta1.TartMachine,
		*infrastructurev1beta1.ResourceReference,
	) error
	DecideProvisionProgress(
		*infrastructurev1beta1.TartHostOperation,
	) (machineexecutionmodel.ProvisionProgressDecisionResult, error)
	PatchProvisionFailureStatus(
		context.Context,
		*infrastructurev1beta1.TartMachine,
		machineexecutionmodel.ProvisionProgressFailed,
	) error
	DecideUpdateOperation(
		context.Context,
		machineexecutionmodel.ProvisionedMachine,
	) (machineexecutionmodel.UpdateOperationDecisionResult, error)
	ApplyUpdateTerminal(
		context.Context,
		*infrastructurev1beta1.TartMachine,
		machineexecutionmodel.UpdateOperationApplyTerminal,
	) error
	ObserveNodeHealth(
		context.Context,
		*infrastructurev1beta1.TartMachine,
	) (machineexecutionmodel.NodeHealthResult, error)
	PlanHealthGateRoute(
		context.Context,
		*infrastructurev1beta1.TartMachine,
		machinehealthdomain.NodeObservation,
	) (machineexecutionmodel.HealthGateRouteResult, error)
	ApplyNodeHealthStatus(
		*infrastructurev1beta1.TartMachine,
		machinehealthdomain.NodeObservation,
	) (machineexecutionmodel.MachineStatusPatchResult, error)
	DecideProvisionHealthGate(
		*infrastructurev1beta1.TartHostOperation,
		machinehealthdomain.NodeObservation,
	) (machineexecutionmodel.ProvisionHealthGateDecisionResult, error)
	CompleteProvision(
		context.Context,
		*infrastructurev1beta1.TartMachine,
		*infrastructurev1beta1.TartHostOperation,
		machinehealthdomain.NodeObservation,
	) error
	SetProvisionHealthPending(
		*infrastructurev1beta1.TartMachine,
		machineexecutionmodel.ProvisionHealthGatePending,
	) (machineexecutionmodel.MachineStatusPatchResult, error)
	DecideUpdateHealthGate(
		*infrastructurev1beta1.TartHostOperation,
		machinehealthdomain.NodeObservation,
	) (machineexecutionmodel.UpdateHealthGateDecisionResult, error)
	CompleteUpdate(
		context.Context,
		*infrastructurev1beta1.TartMachine,
		*infrastructurev1beta1.TartHostOperation,
	) error
	RollbackUpdate(
		context.Context,
		*infrastructurev1beta1.TartMachine,
		*infrastructurev1beta1.TartHostOperation,
		machinehealthdomain.NodeObservation,
	) error
	PatchPlannedMachineStatus(
		context.Context,
		*infrastructurev1beta1.TartMachine,
		machineexecutionmodel.MachineStatusPatchResult,
	) error
}

type CommandHandler struct {
	steps Steps
}

func NewCommandHandler(steps Steps) *CommandHandler {
	return &CommandHandler{steps: steps}
}

func (handler *CommandHandler) HandleMachine(
	ctx context.Context,
	active machineexecutionmodel.ActiveMachine,
	command machinelifecycledomain.MachineCommand,
) error {
	switch command.(type) {
	case machinelifecycledomain.CommandObserveProvisionedMachine:
		return handler.reconcileProvisioned(ctx, machineexecutionmodel.ProvisionedMachine(active))
	case machinelifecycledomain.CommandEnsureProvisionReference:
		return handler.reconcileProvisioning(ctx, machineexecutionmodel.ProvisioningMachine(active))
	default:
		return fmt.Errorf("unknown TartMachine command: %T", command)
	}
}

func (handler *CommandHandler) reconcileProvisioning(
	ctx context.Context,
	provisioning machineexecutionmodel.ProvisioningMachine,
) error {
	referenceResult, err := handler.steps.EnsureProvisionReference(ctx, provisioning)
	if err != nil {
		return err
	}
	switch referenceResult.(type) {
	case machineexecutionmodel.ProvisionReferenceBlocked:
		return nil
	case machineexecutionmodel.ProvisionReferenceReady:
	default:
		return fmt.Errorf("unknown Provision reference result: %T", referenceResult)
	}

	command := machinelifecycledomain.DecideProvision(provisioning.State)
	if err := handler.handleProvision(ctx, provisioning, command); err != nil {
		return err
	}
	return handler.observeNodeHealth(ctx, provisioning.Machine)
}

func (handler *CommandHandler) handleProvision(
	ctx context.Context,
	provisioning machineexecutionmodel.ProvisioningMachine,
	command machinelifecycledomain.ProvisionCommand,
) error {
	switch command.(type) {
	case machinelifecycledomain.CommandStartProvision:
		return handler.steps.StartProvision(ctx, provisioning)
	case machinelifecycledomain.CommandResumeProvisionOperation:
		return handler.resumeProvisionOperation(ctx, provisioning.Machine)
	default:
		return fmt.Errorf("unknown TartMachine provision command: %T", command)
	}
}

func (handler *CommandHandler) resumeProvisionOperation(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	referenceResult, err := handler.steps.ResolveProvisionProgressReference(ctx, machine)
	if err != nil {
		return err
	}
	switch reference := referenceResult.(type) {
	case machineexecutionmodel.ProvisionProgressReferenceStale:
		return handler.steps.ClearStaleProvisionOperationReference(ctx, machine, reference.Reference)
	case machineexecutionmodel.ProvisionProgressReferenceAbsent:
		return nil
	case machineexecutionmodel.ProvisionProgressReferenceResolved:
		return handler.handleProvisionProgress(ctx, machine, reference.Operation)
	default:
		return fmt.Errorf("unknown Operation reference result for provision progress: %T", referenceResult)
	}
}

func (handler *CommandHandler) handleProvisionProgress(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	decision, err := handler.steps.DecideProvisionProgress(operation)
	if err != nil {
		return fmt.Errorf("decide TartHostOperation progress: %w", err)
	}
	switch decision := decision.(type) {
	case machineexecutionmodel.ProvisionProgressFailed:
		return handler.steps.PatchProvisionFailureStatus(ctx, machine, decision)
	case machineexecutionmodel.ProvisionProgressAwaitingHealth:
		return nil
	default:
		return fmt.Errorf("unexpected TartMachine provisioning progress decision: %T", decision)
	}
}

func (handler *CommandHandler) reconcileProvisioned(
	ctx context.Context,
	provisioned machineexecutionmodel.ProvisionedMachine,
) error {
	decision, err := handler.steps.DecideUpdateOperation(ctx, provisioned)
	if err != nil {
		return err
	}
	result, err := handler.handleUpdateOperationDecision(ctx, provisioned.Machine, decision)
	if err != nil {
		return err
	}
	switch result.(type) {
	case machineexecutionmodel.UpdateOperationTerminalHandled:
		return nil
	case machineexecutionmodel.UpdateOperationNeedsNodeHealth:
		return handler.observeNodeHealth(ctx, provisioned.Machine)
	default:
		return fmt.Errorf("unknown Update operation result: %T", result)
	}
}

func (handler *CommandHandler) handleUpdateOperationDecision(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	decision machineexecutionmodel.UpdateOperationDecisionResult,
) (machineexecutionmodel.UpdateOperationStepResult, error) {
	switch decision := decision.(type) {
	case machineexecutionmodel.UpdateOperationApplyTerminal:
		if err := handler.steps.ApplyUpdateTerminal(ctx, machine, decision); err != nil {
			return nil, err
		}
		return machineexecutionmodel.UpdateOperationTerminalHandled{}, nil
	case machineexecutionmodel.UpdateOperationRouteNodeHealth:
		return machineexecutionmodel.UpdateOperationNeedsNodeHealth{}, nil
	default:
		return nil, fmt.Errorf("unknown Update Operation decision result: %T", decision)
	}
}

func (handler *CommandHandler) observeNodeHealth(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	nodeHealth, err := handler.steps.ObserveNodeHealth(ctx, machine)
	if err != nil {
		return err
	}
	switch nodeHealth := nodeHealth.(type) {
	case machineexecutionmodel.NodeHealthUnavailable:
		return nil
	case machineexecutionmodel.NodeHealthObserved:
		return handler.applyObservedHealthGate(ctx, machine, nodeHealth.Observation)
	default:
		return fmt.Errorf("unknown Node health result: %T", nodeHealth)
	}
}

func (handler *CommandHandler) applyObservedHealthGate(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	observation machinehealthdomain.NodeObservation,
) error {
	route, err := handler.steps.PlanHealthGateRoute(ctx, machine, observation)
	if err != nil {
		return err
	}
	patchResult, err := handler.handleHealthGateRoute(ctx, machine, route)
	if err != nil {
		return err
	}
	return handler.steps.PatchPlannedMachineStatus(ctx, machine, patchResult)
}

func (handler *CommandHandler) handleHealthGateRoute(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	route machineexecutionmodel.HealthGateRouteResult,
) (machineexecutionmodel.MachineStatusPatchResult, error) {
	switch route := route.(type) {
	case machineexecutionmodel.HealthGateNodeStatusRoute:
		return handler.steps.ApplyNodeHealthStatus(machine, route.Observation)
	case machineexecutionmodel.HealthGateProvisionRoute:
		return handler.applyProvisionHealthGate(ctx, machine, route.Operation, route.Observation)
	case machineexecutionmodel.HealthGateUpdateRoute:
		return handler.applyUpdateHealthGate(ctx, machine, route.Operation, route.Observation)
	case machineexecutionmodel.HealthGateUpdateTerminalRoute:
		if err := handler.steps.ApplyUpdateTerminal(ctx, machine, machineexecutionmodel.UpdateOperationApplyTerminal(route)); err != nil {
			return nil, err
		}
		return machineexecutionmodel.MachineStatusPatchAlreadyApplied{}, nil
	default:
		return nil, fmt.Errorf("unknown Health Gate route result: %T", route)
	}
}

func (handler *CommandHandler) applyProvisionHealthGate(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) (machineexecutionmodel.MachineStatusPatchResult, error) {
	decision, err := handler.steps.DecideProvisionHealthGate(operation, observation)
	if err != nil {
		return nil, err
	}
	switch decision := decision.(type) {
	case machineexecutionmodel.ProvisionHealthGateComplete:
		original := machine.DeepCopy()
		if err := handler.steps.CompleteProvision(ctx, machine, decision.Operation, decision.Observation); err != nil {
			return nil, err
		}
		return machineexecutionmodel.MachineStatusPatchRequired{Original: original}, nil
	case machineexecutionmodel.ProvisionHealthGatePending:
		return handler.steps.SetProvisionHealthPending(machine, decision)
	default:
		return nil, fmt.Errorf("unknown Provision health gate decision result: %T", decision)
	}
}

func (handler *CommandHandler) applyUpdateHealthGate(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) (machineexecutionmodel.MachineStatusPatchResult, error) {
	decision, err := handler.steps.DecideUpdateHealthGate(operation, observation)
	if err != nil {
		return nil, err
	}
	original := machine.DeepCopy()
	switch decision := decision.(type) {
	case machineexecutionmodel.UpdateHealthGateComplete:
		if err := handler.steps.CompleteUpdate(ctx, machine, decision.Operation); err != nil {
			return nil, err
		}
		return machineexecutionmodel.MachineStatusPatchRequired{Original: original}, nil
	case machineexecutionmodel.UpdateHealthGateRollback:
		if err := handler.steps.RollbackUpdate(ctx, machine, decision.Operation, decision.Observation); err != nil {
			return nil, err
		}
		return machineexecutionmodel.MachineStatusPatchRequired{Original: original}, nil
	default:
		return nil, fmt.Errorf("unknown Update health gate decision result: %T", decision)
	}
}
