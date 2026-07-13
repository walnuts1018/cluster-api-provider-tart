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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	appupdate "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
	applicationhealth "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinehealth"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinelifecycle"
)

func (workflow *Workflow) reconcileUpdateOperationStep(
	ctx context.Context,
	provisioned provisionedMachine,
) (bool, error) {
	machine := provisioned.Machine
	operation, ok, err := workflow.referencedOperation(ctx, machine, "update outcome")
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if !ok || operation.Spec.Type != infrastructurev1beta1.OperationTypeUpdate {
		return false, nil
	}

	command, err := operationCommand(provisioned.State.Provisioned, operation)
	if err != nil {
		return false, fmt.Errorf("decide Update TartHostOperation outcome: %w", err)
	}
	switch command := command.(type) {
	case machinelifecycledomain.CommandApplyUpdateTerminal:
		return true, workflow.applyUpdateTerminalStep(ctx, machine, operation, command.Outcome)
	case machinelifecycledomain.CommandObserveUpdateHealth, machinelifecycledomain.CommandObserveNodeHealth:
		return false, nil
	default:
		return false, fmt.Errorf("unexpected Update TartHostOperation command: %T", command)
	}
}

func (workflow *Workflow) applyUpdateTerminalStep(
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
	if err := workflow.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("set TartMachine Update status: %w", err)
	}
	if outcome == machinelifecycledomain.UpdateOutcomeRolledBack ||
		outcome == machinelifecycledomain.UpdateOutcomeRecoveryRequired {
		trace.SpanFromContext(ctx).SetAttributes(appupdate.FailureTraceAttributes(operation)...)
		workflow.recordUpdateFailureEvent(machine, operation)
	}
	return nil
}

func (workflow *Workflow) completeUpdateStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	if err := workflow.transitionOperationPhase(
		ctx,
		operation,
		infrastructurev1beta1.TartHostOperationPhaseSucceeded,
	); err != nil {
		return err
	}
	machine.Status = appupdate.StatusWithUpdateSucceeded(machine, operation)
	return nil
}

func (workflow *Workflow) rollbackUpdateStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	observation machinehealthdomain.NodeObservation,
) error {
	if err := workflow.transitionUpdateFailurePhase(
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

func (workflow *Workflow) recordUpdateFailureEvent(
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
) {
	if workflow.Recorder == nil {
		return
	}
	condition := appupdate.FailureCondition(operation)
	if condition == nil {
		return
	}
	workflow.Recorder.Eventf(
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
