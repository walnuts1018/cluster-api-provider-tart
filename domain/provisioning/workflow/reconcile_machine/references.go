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
	machineexecutionstep "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/step/machineexecution"
	model "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/step/machineexecution/model"
)

func (steps *workflowRuntime) resolveOperationReferenceStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	purpose string,
) (model.OperationReferenceResult, error) {
	if machine.Status.OperationRef == nil {
		return model.OperationReferenceAbsent{}, nil
	}
	operation := &infrastructurev1beta1.TartHostOperation{}
	key := operationKey(machine.Status.OperationRef)
	if err := steps.Get(ctx, key, operation); err != nil {
		if apierrors.IsNotFound(err) {
			return model.OperationReferenceStale{Reference: machine.Status.OperationRef.DeepCopy()}, nil
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
	return model.OperationReferenceResolved{Operation: operation}, nil
}

func (steps *workflowRuntime) resolveHostReferenceStep(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	purpose string,
) (model.HostReferenceResult, error) {
	if machine.Status.HostRef == nil {
		return model.HostReferenceMissing{}, nil
	}
	host := &infrastructurev1beta1.TartHost{}
	hostKey := client.ObjectKey{
		Namespace: machine.Status.HostRef.Namespace,
		Name:      machine.Status.HostRef.Name,
	}
	if err := steps.Get(ctx, hostKey, host); err != nil {
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
	return model.HostReferenceResolved{Host: host}, nil
}

func resolvedHost(
	reference model.HostReferenceResult,
	purpose string,
) (*infrastructurev1beta1.TartHost, error) {
	switch reference := reference.(type) {
	case model.HostReferenceResolved:
		return reference.Host, nil
	case model.HostReferenceMissing:
		return nil, fmt.Errorf("TartHost reference is missing for %s", purpose)
	default:
		return nil, fmt.Errorf("unknown TartHost reference result for %s: %T", purpose, reference)
	}
}

func (steps *workflowRuntime) transitionOperationPhase(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	target infrastructurev1beta1.TartHostOperationPhase,
) error {
	patchResult := machineexecutionstep.PlanOperationPhaseTransition(operation, target)
	return steps.patchPlannedOperationStatusStep(ctx, operation, patchResult, "set TartHostOperation phase")
}

func (steps *workflowRuntime) patchPlannedOperationStatusStep(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	patchResult model.OperationStatusPatchResult,
	purpose string,
) error {
	switch patchResult := patchResult.(type) {
	case model.OperationStatusPatchAlreadyApplied:
		return nil
	case model.OperationStatusPatchRequired:
		if err := steps.Status().Patch(ctx, operation, client.MergeFrom(patchResult.Original)); err != nil {
			return fmt.Errorf("%s: %w", purpose, err)
		}
		return nil
	default:
		return fmt.Errorf("unknown TartHostOperation status patch result: %T", patchResult)
	}
}

func (steps *workflowRuntime) transitionUpdateFailurePhase(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	failedPhase infrastructurev1beta1.TartHostOperationPhase,
	target infrastructurev1beta1.TartHostOperationPhase,
) error {
	patchResult := machineexecutionstep.PlanUpdateFailurePhaseTransition(operation, failedPhase, target)
	return steps.patchPlannedOperationStatusStep(ctx, operation, patchResult, "set TartHostOperation update failure phase")
}

func operationKey(ref *infrastructurev1beta1.ResourceReference) types.NamespacedName {
	if ref == nil {
		return types.NamespacedName{}
	}
	return types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}
}
