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

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	appupdate "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
)

func (workflow *Workflow) referencedOperation(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	purpose string,
) (*infrastructurev1beta1.TartHostOperation, bool, error) {
	if machine.Status.OperationRef == nil {
		return nil, false, nil
	}
	operation := &infrastructurev1beta1.TartHostOperation{}
	key := operationKey(machine.Status.OperationRef)
	if err := workflow.Get(ctx, key, operation); err != nil {
		return nil, false, fmt.Errorf("get TartHostOperation for %s: %w", purpose, err)
	}
	if operation.UID != machine.Status.OperationRef.UID {
		return nil, false, fmt.Errorf(
			"TartHostOperation UID mismatch for %s: expected %s, got %s",
			purpose,
			machine.Status.OperationRef.UID,
			operation.UID,
		)
	}
	return operation, true, nil
}

func (workflow *Workflow) referencedHost(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	purpose string,
) (*infrastructurev1beta1.TartHost, error) {
	if machine.Status.HostRef == nil {
		return nil, fmt.Errorf("TartHost reference is missing for %s", purpose)
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
	return host, nil
}

func (workflow *Workflow) transitionOperationPhase(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	target infrastructurev1beta1.TartHostOperationPhase,
) error {
	original := operation.DeepCopy()
	operation.Status.Phase = target
	if operation.Status.ObservedGeneration < operation.Generation {
		operation.Status.ObservedGeneration = operation.Generation
	}
	if err := workflow.Status().Patch(ctx, operation, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("set TartHostOperation phase: %w", err)
	}
	return nil
}

func (workflow *Workflow) transitionUpdateFailurePhase(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	failedPhase infrastructurev1beta1.TartHostOperationPhase,
	target infrastructurev1beta1.TartHostOperationPhase,
) error {
	original := operation.DeepCopy()
	operation.Status.Phase = target
	appupdate.UpdateFailureCondition(&operation.Status, operation.Generation, failedPhase, target)
	if operation.Status.ObservedGeneration < operation.Generation {
		operation.Status.ObservedGeneration = operation.Generation
	}
	if err := workflow.Status().Patch(ctx, operation, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("set TartHostOperation update failure phase: %w", err)
	}
	return nil
}

func operationKey(ref *infrastructurev1beta1.ResourceReference) types.NamespacedName {
	if ref == nil {
		return types.NamespacedName{}
	}
	return types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}
}
