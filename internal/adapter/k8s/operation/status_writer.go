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

package operation

import (
	"context"

	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	appupdate "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
	inplaceupdatedomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/inplaceupdate"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
	slotdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/slot"
)

type StatusWriter struct {
	client client.Client
}

func NewStatusWriter(k8sClient client.Client) *StatusWriter {
	return &StatusWriter{client: k8sClient}
}

func (writer *StatusWriter) TransitionPhase(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	target infrastructurev1beta1.TartHostOperationPhase,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHostOperation{}
		if err := writer.client.Get(ctx, client.ObjectKeyFromObject(operation), current); err != nil {
			return err
		}
		original := current.DeepCopy()
		current.Status.Phase = target
		if current.Status.ObservedGeneration < current.Generation {
			current.Status.ObservedGeneration = current.Generation
		}
		return writer.client.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

func (writer *StatusWriter) HandleBootTrialDeadline(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	targetSlot, err := slotdomain.Parse(string(operation.Spec.TargetSlot))
	if err != nil {
		return err
	}
	activeSlot, err := targetSlot.Inactive()
	if err != nil {
		return err
	}
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHostOperation{}
		if err := writer.client.Get(ctx, client.ObjectKeyFromObject(operation), current); err != nil {
			return err
		}
		decision, err := inplaceupdatedomain.Transition(inplaceupdatedomain.State{
			Phase:      operationdomain.PhaseBootTrial,
			ActiveSlot: activeSlot,
			TargetSlot: targetSlot,
			Attempt:    current.Status.Attempt,
		}, inplaceupdatedomain.EventBootFailed)
		if err != nil {
			return err
		}
		original := current.DeepCopy()
		current.Status.Attempt = decision.Attempt
		current.Status.Phase = infrastructurev1beta1.TartHostOperationPhase(decision.Phase)
		appupdate.SetUpdateFailureCondition(
			&current.Status,
			current.Generation,
			infrastructurev1beta1.TartHostOperationPhaseBootTrial,
			current.Status.Phase,
		)
		if current.Status.ObservedGeneration < current.Generation {
			current.Status.ObservedGeneration = current.Generation
		}
		return writer.client.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

func (writer *StatusWriter) TransitionUpdateFailure(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	failedPhase infrastructurev1beta1.TartHostOperationPhase,
	target infrastructurev1beta1.TartHostOperationPhase,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHostOperation{}
		if err := writer.client.Get(ctx, client.ObjectKeyFromObject(operation), current); err != nil {
			return err
		}
		original := current.DeepCopy()
		current.Status.Phase = target
		appupdate.UpdateFailureCondition(&current.Status, current.Generation, failedPhase, target)
		if current.Status.ObservedGeneration < current.Generation {
			current.Status.ObservedGeneration = current.Generation
		}
		return writer.client.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}
