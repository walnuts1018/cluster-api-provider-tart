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

package operationexecution

import (
	"context"
	"fmt"
	"time"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/operation"
)

func mapCommand(
	ctx context.Context,
	reader ResourceReader,
	operation *infrastructurev1beta1.TartHostOperation,
	now time.Time,
) (operationdomain.Command, error) {
	kind, err := operationdomain.ParseKind(string(operation.Spec.Type))
	if err != nil {
		return operationdomain.Command{}, err
	}
	var phase operationdomain.Phase
	if operation.Status.Phase != "" {
		phase, err = operationdomain.ParsePhase(string(operation.Status.Phase))
		if err != nil {
			return operationdomain.Command{}, err
		}
	}
	cleaningPolicy, err := mapCleaningPolicy(ctx, reader, operation)
	if err != nil {
		return operationdomain.Command{}, err
	}
	return operationdomain.Command{
		Kind:           kind,
		Phase:          phase,
		CleaningPolicy: cleaningPolicy,
		Deadline:       operation.Spec.Deadline.Time,
		Now:            now,
	}, nil
}

func mapCleaningPolicy(
	ctx context.Context,
	reader ResourceReader,
	operation *infrastructurev1beta1.TartHostOperation,
) (operationdomain.CleaningPolicy, error) {
	switch operation.Spec.Type {
	case infrastructurev1beta1.OperationTypeWipeAll:
		return operationdomain.CleaningPolicyWipeAll, nil
	case infrastructurev1beta1.OperationTypeClean:
		if operation.Spec.MachineRef == nil {
			return operationdomain.CleaningPolicyUnspecified, fmt.Errorf("machineRef is required for Clean operation")
		}
		machine, err := reader.GetMachine(ctx, *operation.Spec.MachineRef)
		if err != nil {
			return operationdomain.CleaningPolicyUnspecified, err
		}
		return domainCleaningPolicy(machine.Spec.DeletionPolicy), nil
	case infrastructurev1beta1.OperationTypeProvision,
		infrastructurev1beta1.OperationTypeUpdate,
		infrastructurev1beta1.OperationTypeRollback,
		infrastructurev1beta1.OperationTypeRecovery:
		return operationdomain.CleaningPolicyUnspecified, nil
	default:
		return operationdomain.CleaningPolicyUnspecified, nil
	}
}

func domainCleaningPolicy(policy infrastructurev1beta1.DeletionPolicy) operationdomain.CleaningPolicy {
	switch policy {
	case infrastructurev1beta1.DeletionPolicyRetainData:
		return operationdomain.CleaningPolicyRetainData
	case infrastructurev1beta1.DeletionPolicyRetainState:
		return operationdomain.CleaningPolicyRetainState
	case infrastructurev1beta1.DeletionPolicyWipeAll:
		return operationdomain.CleaningPolicyWipeAll
	default:
		return operationdomain.CleaningPolicyUnspecified
	}
}

func apiCleaningPolicy(policy operationdomain.CleaningPolicy) infrastructurev1beta1.DeletionPolicy {
	switch policy {
	case operationdomain.CleaningPolicyRetainData:
		return infrastructurev1beta1.DeletionPolicyRetainData
	case operationdomain.CleaningPolicyRetainState:
		return infrastructurev1beta1.DeletionPolicyRetainState
	case operationdomain.CleaningPolicyWipeAll:
		return infrastructurev1beta1.DeletionPolicyWipeAll
	case operationdomain.CleaningPolicyUnspecified:
		return ""
	default:
		return ""
	}
}

func apiOperationPhase(phase operationdomain.Phase) infrastructurev1beta1.TartHostOperationPhase {
	return infrastructurev1beta1.TartHostOperationPhase(phase)
}
