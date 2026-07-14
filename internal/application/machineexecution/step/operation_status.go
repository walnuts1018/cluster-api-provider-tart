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

package step

import (
	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	appupdate "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution/model"
)

func PlanOperationPhaseTransition(
	operation *infrastructurev1beta1.TartHostOperation,
	target infrastructurev1beta1.TartHostOperationPhase,
) model.OperationStatusPatchResult {
	original := operation.DeepCopy()
	statusChanged := operation.Status.Phase != target
	operation.Status.Phase = target
	if operation.Status.ObservedGeneration < operation.Generation {
		statusChanged = true
		operation.Status.ObservedGeneration = operation.Generation
	}
	if !statusChanged {
		return model.OperationStatusPatchAlreadyApplied{}
	}
	return model.OperationStatusPatchRequired{Original: original}
}

func PlanUpdateFailurePhaseTransition(
	operation *infrastructurev1beta1.TartHostOperation,
	failedPhase infrastructurev1beta1.TartHostOperationPhase,
	target infrastructurev1beta1.TartHostOperationPhase,
) model.OperationStatusPatchResult {
	original := operation.DeepCopy()
	operation.Status.Phase = target
	appupdate.UpdateFailureCondition(&operation.Status, operation.Generation, failedPhase, target)
	if operation.Status.ObservedGeneration < operation.Generation {
		operation.Status.ObservedGeneration = operation.Generation
	}
	return model.OperationStatusPatchRequired{Original: original}
}
