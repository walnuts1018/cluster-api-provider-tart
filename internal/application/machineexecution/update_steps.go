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

	"go.opentelemetry.io/otel/trace"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	appupdate "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
	machineexecutionstep "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution/step"
	applicationhealth "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinehealth"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinelifecycle"
)

func (steps *StepExecutor) reconcileUpdateOperationStep(
	ctx context.Context,
	provisioned provisionedMachine,
) (updateOperationStepResult, error) {
	machine := provisioned.Machine
	operationReference, err := steps.resolveOperationReferenceStep(ctx, machine, "update outcome")
	if err != nil {
		return nil, err
	}
	switch reference := operationReference.(type) {
	case operationReferenceAbsent, operationReferenceStale:
		return updateOperationNeedsNodeHealth{}, nil
	case operationReferenceResolved:
		if reference.Operation.Spec.Type != infrastructurev1beta1.OperationTypeUpdate {
			return updateOperationNeedsNodeHealth{}, nil
		}
		return steps.reconcileResolvedUpdateOperationStep(ctx, provisioned, reference.Operation)
	default:
		return nil, fmt.Errorf("unknown Operation reference result for update outcome: %T", operationReference)
	}
}

func (steps *StepExecutor) reconcileResolvedUpdateOperationStep(
	ctx context.Context,
	provisioned provisionedMachine,
	operation *infrastructurev1beta1.TartHostOperation,
) (updateOperationStepResult, error) {
	decision, err := decideUpdateOperationStep(operation)
	if err != nil {
		return nil, err
	}
	return steps.applyUpdateOperationDecisionStep(ctx, provisioned.Machine, decision)
}

func decideUpdateOperationStep(
	operation *infrastructurev1beta1.TartHostOperation,
) (updateOperationDecisionResult, error) {
	route, err := machineexecutionstep.DecideOperationRoute(machineexecutionstep.OperationProvisioned{}, operation)
	if err != nil {
		return nil, fmt.Errorf("decide Update TartHostOperation outcome: %w", err)
	}
	switch route := route.(type) {
	case machineexecutionstep.OperationUpdateTerminalRoute:
		return updateOperationApplyTerminal{Operation: route.Operation, Outcome: route.Outcome}, nil
	case machineexecutionstep.OperationUpdateHealthRoute, machineexecutionstep.OperationNodeHealthRoute:
		return updateOperationRouteNodeHealth{}, nil
	default:
		return nil, fmt.Errorf("unknown provisioned TartMachine route for Update Operation: %T", route)
	}
}

func (steps *StepExecutor) applyUpdateOperationDecisionStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	decision updateOperationDecisionResult,
) (updateOperationStepResult, error) {
	switch decision := decision.(type) {
	case updateOperationApplyTerminal:
		if err := steps.applyUpdateTerminalStep(ctx, machine, decision.Operation, decision.Outcome); err != nil {
			return nil, err
		}
		return updateOperationTerminalHandled{}, nil
	case updateOperationRouteNodeHealth:
		return updateOperationNeedsNodeHealth{}, nil
	default:
		return nil, fmt.Errorf("unknown Update Operation decision result: %T", decision)
	}
}

func (steps *StepExecutor) applyUpdateTerminalStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	outcome machinelifecycledomain.UpdateOutcome,
) error {
	original := machine.DeepCopy()
	switch outcome {
	case machinelifecycledomain.UpdateOutcomeSucceeded:
		machine.Status = appupdate.StatusWithUpdateSucceeded(machine, operation)
		appupdate.RecordUpdateOutcome(ctx, operation, "succeeded")
	case machinelifecycledomain.UpdateOutcomeRolledBack:
		machine.Status = appupdate.StatusWithUpdateRolledBack(machine, operation)
		appupdate.RecordUpdateOutcome(ctx, operation, "failed")
	case machinelifecycledomain.UpdateOutcomeRecoveryRequired:
		machine.Status = appupdate.StatusWithUpdateRecoveryRequired(machine, operation)
		appupdate.RecordUpdateOutcome(ctx, operation, "recovery_required")
	default:
		return fmt.Errorf("unknown TartMachine update outcome: %q", outcome)
	}
	if err := steps.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("set TartMachine Update status: %w", err)
	}
	if outcome == machinelifecycledomain.UpdateOutcomeRolledBack ||
		outcome == machinelifecycledomain.UpdateOutcomeRecoveryRequired {
		trace.SpanFromContext(ctx).SetAttributes(appupdate.FailureTraceAttributes(operation)...)
		steps.recordUpdateFailureEvent(machine, operation)
	}
	return nil
}

func (steps *StepExecutor) completeUpdateStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	if err := steps.transitionOperationPhase(
		ctx,
		operation,
		infrastructurev1beta1.TartHostOperationPhaseSucceeded,
	); err != nil {
		return err
	}
	machine.Status = appupdate.StatusWithUpdateSucceeded(machine, operation)
	return nil
}

func (steps *StepExecutor) rollbackUpdateStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) error {
	if err := steps.transitionUpdateFailurePhase(
		ctx,
		operation,
		infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
		infrastructurev1beta1.TartHostOperationPhaseRollingBack,
	); err != nil {
		return err
	}
	machine.Status = applicationhealth.StatusWithNodeHealth(machine, observation)
	return nil
}

func (steps *StepExecutor) recordUpdateFailureEvent(
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
) {
	if steps.Recorder == nil {
		return
	}
	condition := appupdate.FailureCondition(operation)
	if condition == nil {
		return
	}
	steps.Recorder.Eventf(
		machine,
		"Warning",
		"UpdateFailed",
		"operationID=%s host=%s operationType=%s failureReason=%s message=%s",
		operation.Spec.OperationID,
		operation.Spec.HostRef.Name,
		operation.Spec.Type,
		condition.Reason,
		condition.Message,
	)
}
