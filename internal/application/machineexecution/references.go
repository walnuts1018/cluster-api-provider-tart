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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	appupdate "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
)

func (workflow *Workflow) resolveOperationReferenceStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	purpose string,
) (operationReferenceResult, error) {
	if machine.Status.OperationRef == nil {
		return operationReferenceAbsent{}, nil
	}
	operation := &infrastructurev1beta1.TartHostOperation{}
	key := operationKey(machine.Status.OperationRef)
	if err := workflow.Get(ctx, key, operation); err != nil {
		if apierrors.IsNotFound(err) {
			return operationReferenceStale{Reference: machine.Status.OperationRef.DeepCopy()}, nil
		}
		return nil, fmt.Errorf("get TartHostOperation for %s: %w", purpose, err)
	}
	if operation.UID != machine.Status.OperationRef.UID {
		return nil, fmt.Errorf(
			"TartHostOperation UID mismatch for %s: expected %s, got %s",
			purpose,
			machine.Status.OperationRef.UID,
			operation.UID,
		)
	}
	return operationReferenceResolved{Operation: operation}, nil
}

func (workflow *Workflow) resolveHostReferenceStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	purpose string,
) (hostReferenceResult, error) {
	if machine.Status.HostRef == nil {
		return hostReferenceMissing{}, nil
	}
	host := &infrastructurev1beta1.TartHost{}
	hostKey := client.ObjectKey{
		Namespace: machine.Status.HostRef.Namespace,
		Name:      machine.Status.HostRef.Name,
	}
	if err := workflow.Get(ctx, hostKey, host); err != nil {
		return nil, fmt.Errorf("get TartHost for %s: %w", purpose, err)
	}
	if host.UID != machine.Status.HostRef.UID {
		return nil, fmt.Errorf(
			"TartHost UID mismatch for %s: expected %s, got %s",
			purpose,
			machine.Status.HostRef.UID,
			host.UID,
		)
	}
	return hostReferenceResolved{Host: host}, nil
}

func resolvedHost(
	reference hostReferenceResult,
	purpose string,
) (*infrastructurev1beta1.TartHost, error) {
	switch reference := reference.(type) {
	case hostReferenceResolved:
		return reference.Host, nil
	case hostReferenceMissing:
		return nil, fmt.Errorf("TartHost reference is missing for %s", purpose)
	default:
		return nil, fmt.Errorf("unknown TartHost reference result for %s: %T", purpose, reference)
	}
}

func (workflow *Workflow) transitionOperationPhase(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	target infrastructurev1beta1.TartHostOperationPhase,
) error {
	patchResult := planOperationPhaseTransition(operation, target)
	return workflow.patchPlannedOperationStatusStep(ctx, operation, patchResult, "set TartHostOperation phase")
}

func planOperationPhaseTransition(
	operation *infrastructurev1beta1.TartHostOperation,
	target infrastructurev1beta1.TartHostOperationPhase,
) operationStatusPatchResult {
	original := operation.DeepCopy()
	statusChanged := operation.Status.Phase != target
	operation.Status.Phase = target
	if operation.Status.ObservedGeneration < operation.Generation {
		statusChanged = true
		operation.Status.ObservedGeneration = operation.Generation
	}
	if !statusChanged {
		return operationStatusPatchAlreadyApplied{}
	}
	return operationStatusPatchRequired{Original: original}
}

func (workflow *Workflow) patchPlannedOperationStatusStep(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	patchResult operationStatusPatchResult,
	purpose string,
) error {
	switch patchResult := patchResult.(type) {
	case operationStatusPatchAlreadyApplied:
		return nil
	case operationStatusPatchRequired:
		if err := workflow.Status().Patch(ctx, operation, client.MergeFrom(patchResult.Original)); err != nil {
			return fmt.Errorf("%s: %w", purpose, err)
		}
		return nil
	default:
		return fmt.Errorf("unknown TartHostOperation status patch result: %T", patchResult)
	}
}

func (workflow *Workflow) transitionUpdateFailurePhase(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	failedPhase infrastructurev1beta1.TartHostOperationPhase,
	target infrastructurev1beta1.TartHostOperationPhase,
) error {
	patchResult := planUpdateFailurePhaseTransition(operation, failedPhase, target)
	return workflow.patchPlannedOperationStatusStep(ctx, operation, patchResult, "set TartHostOperation update failure phase")
}

func planUpdateFailurePhaseTransition(
	operation *infrastructurev1beta1.TartHostOperation,
	failedPhase infrastructurev1beta1.TartHostOperationPhase,
	target infrastructurev1beta1.TartHostOperationPhase,
) operationStatusPatchResult {
	original := operation.DeepCopy()
	operation.Status.Phase = target
	appupdate.UpdateFailureCondition(&operation.Status, operation.Generation, failedPhase, target)
	if operation.Status.ObservedGeneration < operation.Generation {
		operation.Status.ObservedGeneration = operation.Generation
	}
	return operationStatusPatchRequired{Original: original}
}

func operationKey(ref *infrastructurev1beta1.ResourceReference) types.NamespacedName {
	if ref == nil {
		return types.NamespacedName{}
	}
	return types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}
}
