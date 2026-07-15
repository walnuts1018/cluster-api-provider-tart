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

package inplaceupdate

import (
	"context"
	"fmt"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/telemetry"
)

const (
	ConditionDegraded       = "Degraded"
	reasonBootFailed        = "BootFailed"
	reasonHealthCheckFailed = "HealthCheckFailed"
	reasonRecoveryRequired  = "RecoveryRequired"
)

func SetUpdateFailureCondition(
	status *infrastructurev1beta1.TartHostOperationStatus,
	generation int64,
	failedPhase infrastructurev1beta1.TartHostOperationPhase,
	targetPhase infrastructurev1beta1.TartHostOperationPhase,
) {
	reason := failureReason(failedPhase)
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               ConditionDegraded,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            failureMessage(reason, failedPhase, targetPhase),
		ObservedGeneration: generation,
	})
}

func UpdateFailureCondition(
	status *infrastructurev1beta1.TartHostOperationStatus,
	generation int64,
	fallbackPhase infrastructurev1beta1.TartHostOperationPhase,
	targetPhase infrastructurev1beta1.TartHostOperationPhase,
) {
	failedPhase := failurePhaseFromCondition(status)
	if failedPhase == "" {
		failedPhase = fallbackPhase
	}
	SetUpdateFailureCondition(status, generation, failedPhase, targetPhase)
}

func FailureCondition(operation *infrastructurev1beta1.TartHostOperation) *metav1.Condition {
	if operation == nil {
		return nil
	}
	return apimeta.FindStatusCondition(operation.Status.Conditions, ConditionDegraded)
}

func FailureTraceAttributes(operation *infrastructurev1beta1.TartHostOperation) []attribute.KeyValue {
	if operation == nil {
		return nil
	}
	attributes := []attribute.KeyValue{
		attribute.String("tart.operation.id", operation.Spec.OperationID),
		attribute.String("tart.operation.type", string(operation.Spec.Type)),
		attribute.String("tart.operation.phase", string(operation.Status.Phase)),
	}
	if condition := FailureCondition(operation); condition != nil {
		attributes = append(attributes,
			attribute.String("tart.update.failure_reason", condition.Reason),
			attribute.String("tart.update.failure_message", condition.Message),
		)
	}
	return attributes
}

func RecordUpdateOutcome(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	result string,
) {
	counter, err := telemetry.Meter.Int64Counter("tart.inplaceupdate.operations")
	if err != nil || operation == nil {
		return
	}
	counter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("operation_type", string(operation.Spec.Type)),
		attribute.String("phase", string(operation.Status.Phase)),
		attribute.String("result", result),
		attribute.Bool("rollback", operation.Status.Phase == infrastructurev1beta1.TartHostOperationPhaseFailed),
	))
}

func failureReason(phase infrastructurev1beta1.TartHostOperationPhase) string {
	switch phase {
	case infrastructurev1beta1.TartHostOperationPhaseWriting:
		return "ArtifactWriteFailed"
	case infrastructurev1beta1.TartHostOperationPhaseVerifying:
		return "ArtifactVerificationFailed"
	case infrastructurev1beta1.TartHostOperationPhaseBootTrial:
		return reasonBootFailed
	case infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth:
		return reasonHealthCheckFailed
	case infrastructurev1beta1.TartHostOperationPhaseRollingBack:
		return reasonRecoveryRequired
	case infrastructurev1beta1.TartHostOperationPhasePending,
		infrastructurev1beta1.TartHostOperationPhasePreparingBoot,
		infrastructurev1beta1.TartHostOperationPhaseWaitingForAgent,
		infrastructurev1beta1.TartHostOperationPhaseDistributionUpdating,
		infrastructurev1beta1.TartHostOperationPhaseSucceeded,
		infrastructurev1beta1.TartHostOperationPhaseFailed,
		infrastructurev1beta1.TartHostOperationPhaseRecoveryRequired:
		return "UpdateFailed"
	default:
		return "UpdateFailed"
	}
}

func failureMessage(
	reason string,
	failedPhase infrastructurev1beta1.TartHostOperationPhase,
	targetPhase infrastructurev1beta1.TartHostOperationPhase,
) string {
	switch targetPhase {
	case infrastructurev1beta1.TartHostOperationPhaseRollingBack:
		return fmt.Sprintf("In-place OS update failed during %s and rollback has started", failedPhase)
	case infrastructurev1beta1.TartHostOperationPhaseFailed:
		return fmt.Sprintf("In-place OS update failed during %s and the previous slot is healthy", failedPhase)
	case infrastructurev1beta1.TartHostOperationPhaseRecoveryRequired:
		if reason == reasonRecoveryRequired {
			return "In-place OS update failed and automatic rollback did not restore health"
		}
		return fmt.Sprintf("In-place OS update failed during %s and automatic rollback did not restore health", failedPhase)
	case infrastructurev1beta1.TartHostOperationPhasePending,
		infrastructurev1beta1.TartHostOperationPhasePreparingBoot,
		infrastructurev1beta1.TartHostOperationPhaseWaitingForAgent,
		infrastructurev1beta1.TartHostOperationPhaseWriting,
		infrastructurev1beta1.TartHostOperationPhaseVerifying,
		infrastructurev1beta1.TartHostOperationPhaseBootTrial,
		infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
		infrastructurev1beta1.TartHostOperationPhaseDistributionUpdating,
		infrastructurev1beta1.TartHostOperationPhaseSucceeded:
		return "In-place OS update failed"
	default:
		return "In-place OS update failed"
	}
}

func failurePhaseFromCondition(status *infrastructurev1beta1.TartHostOperationStatus) infrastructurev1beta1.TartHostOperationPhase {
	if status == nil {
		return ""
	}
	condition := apimeta.FindStatusCondition(status.Conditions, ConditionDegraded)
	if condition == nil {
		return ""
	}
	switch condition.Reason {
	case "ArtifactWriteFailed":
		return infrastructurev1beta1.TartHostOperationPhaseWriting
	case "ArtifactVerificationFailed":
		return infrastructurev1beta1.TartHostOperationPhaseVerifying
	case "BootFailed":
		return infrastructurev1beta1.TartHostOperationPhaseBootTrial
	case "HealthCheckFailed":
		return infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth
	case "RecoveryRequired":
		return infrastructurev1beta1.TartHostOperationPhaseRollingBack
	default:
		return ""
	}
}
