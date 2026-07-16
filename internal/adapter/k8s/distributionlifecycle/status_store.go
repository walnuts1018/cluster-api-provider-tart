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

package distributionlifecycle

import (
	"context"
	"fmt"

	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	application "github.com/walnuts1018/cluster-api-provider-tart/internal/application/distributionlifecycle"
	appupdate "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
)

// LifecyclePhaseはDistribution Lifecycleの大まかな実行区間をStatusへ保存する値である。
type LifecyclePhase string

const (
	LifecyclePhasePreflight LifecyclePhase = "Preflight"
	LifecyclePhaseSnapshot  LifecyclePhase = "Snapshot"
	LifecyclePhaseApply     LifecyclePhase = "Apply"
	LifecyclePhaseVerify    LifecyclePhase = "Verify"
)

// StatusStoreはDistribution Lifecycleの冪等Step結果をTartHostOperation Statusへ保存する。
type StatusStore struct {
	client client.Client
}

func NewStatusStore(k8sClient client.Client) *StatusStore {
	return &StatusStore{client: k8sClient}
}

// RecordStepはStep成功直後に呼び出し、completedStepsと必要なSnapshotRefを永続化する。
func (store *StatusStore) RecordStep(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	plan domain.Plan,
	step domain.Step,
	snapshotRef *infrastructurev1beta1.ResourceReference,
) error {
	if store.client == nil {
		return fmt.Errorf("kubernetes client is required")
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHostOperation{}
		if err := store.client.Get(ctx, client.ObjectKeyFromObject(operation), current); err != nil {
			return fmt.Errorf("get TartHostOperation for lifecycle status: %w", err)
		}
		if current.UID != operation.UID || current.Spec.OperationID != operation.Spec.OperationID {
			return fmt.Errorf("TartHostOperation identity changed while recording lifecycle step")
		}

		planForStatus := plan
		if current.Status.SnapshotRef != nil {
			planForStatus.SnapshotRef = current.Status.SnapshotRef.Name
		}
		if snapshotRef != nil {
			planForStatus.SnapshotRef = snapshotRef.Name
		}
		nextSteps, decision, err := application.RecordCompletedStep(current.Status.CompletedSteps, step, planForStatus)
		if err != nil {
			return err
		}
		if decision.AlreadyCompleted {
			return nil
		}
		if step == domain.StepSnapshotCreated && snapshotRef == nil && current.Status.SnapshotRef == nil {
			return fmt.Errorf("SnapshotRef is required when recording SnapshotCreated")
		}
		if snapshotRef != nil && current.Status.SnapshotRef != nil && *current.Status.SnapshotRef != *snapshotRef {
			return fmt.Errorf("SnapshotRef conflicts with existing lifecycle status")
		}

		original := current.DeepCopy()
		current.Status.CompletedSteps = nextSteps
		current.Status.LifecyclePhase = string(phaseForStep(step))
		if snapshotRef != nil {
			current.Status.SnapshotRef = snapshotRef.DeepCopy()
		}
		if step == domain.StepCommitted {
			current.Status.Phase = infrastructurev1beta1.TartHostOperationPhaseSucceeded
		} else if current.Status.Phase == "" {
			current.Status.Phase = infrastructurev1beta1.TartHostOperationPhaseDistributionUpdating
		}
		current.Status.ObservedGeneration = current.Generation

		return store.client.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

// MarkStepFailureはNode Lifecycle Step失敗時に更新クラスごとの失敗遷移を永続化する。
func (store *StatusStore) MarkStepFailure(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	if store.client == nil {
		return fmt.Errorf("kubernetes client is required")
	}
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHostOperation{}
		if err := store.client.Get(ctx, client.ObjectKeyFromObject(operation), current); err != nil {
			return fmt.Errorf("get TartHostOperation for lifecycle failure status: %w", err)
		}
		if current.UID != operation.UID || current.Spec.OperationID != operation.Spec.OperationID {
			return fmt.Errorf("TartHostOperation identity changed while recording lifecycle failure status")
		}

		targetPhase := infrastructurev1beta1.TartHostOperationPhaseRollingBack
		if current.Spec.UpdateClass == infrastructurev1beta1.UpdateClassStateMigration {
			if current.Status.SnapshotRef == nil {
				return fmt.Errorf("SnapshotRef is required before marking StateMigration recovery")
			}
			targetPhase = infrastructurev1beta1.TartHostOperationPhaseRecoveryRequired
		}

		original := current.DeepCopy()
		current.Status.Phase = targetPhase
		appupdate.UpdateFailureCondition(
			&current.Status,
			current.Generation,
			infrastructurev1beta1.TartHostOperationPhaseDistributionUpdating,
			targetPhase,
		)
		current.Status.ObservedGeneration = current.Generation
		return store.client.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

func phaseForStep(step domain.Step) LifecyclePhase {
	switch step {
	case domain.StepPreflightCompleted:
		return LifecyclePhasePreflight
	case domain.StepSnapshotCreated:
		return LifecyclePhaseSnapshot
	case domain.StepTargetSlotWritten, domain.StepKubeadmApplied, domain.StepTargetSlotBooted, domain.StepCommitted:
		return LifecyclePhaseApply
	case domain.StepHealthVerified:
		return LifecyclePhaseVerify
	default:
		return ""
	}
}
