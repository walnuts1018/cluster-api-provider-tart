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
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/entity/machinelifecycle"
	machineexecutionstep "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/step/machineexecution"
	model "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/step/machineexecution/model"
	appupdate "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/update_machine"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/machinehealth"
)

func (steps *workflowRuntime) decideUpdateOperationStep(
	ctx context.Context,
	provisioned provisionedMachine,
) (model.UpdateOperationDecisionResult, error) {
	machine := provisioned.Machine
	operationReference, err := steps.resolveOperationReferenceStep(ctx, machine, "update outcome")
	if err != nil {
		return nil, err
	}
	switch reference := operationReference.(type) {
	case model.OperationReferenceAbsent, model.OperationReferenceStale:
		return model.UpdateOperationRouteNodeHealth{}, nil
	case model.OperationReferenceResolved:
		if reference.Operation.Spec.Type != infrastructurev1beta1.OperationTypeUpdate {
			return model.UpdateOperationRouteNodeHealth{}, nil
		}
		return machineexecutionstep.DecideUpdateOperation(reference.Operation)
	default:
		return nil, fmt.Errorf("unknown Operation reference result for update outcome: %T", operationReference)
	}
}

func (steps *workflowRuntime) applyUpdateTerminalStep(
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

func (steps *workflowRuntime) completeUpdateStep(
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

func (steps *workflowRuntime) rollbackUpdateStep(
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
	machine.Status = machineexecutionstep.StatusWithNodeHealth(machine, observation)
	return nil
}

func (steps *workflowRuntime) recordUpdateFailureEvent(
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
		nil,
		"Warning",
		"UpdateFailed",
		"UpdateFailed",
		"operationID=%s host=%s operationType=%s failureReason=%s message=%s",
		operation.Spec.OperationID,
		operation.Spec.HostRef.Name,
		operation.Spec.Type,
		condition.Reason,
		condition.Message,
	)
}
