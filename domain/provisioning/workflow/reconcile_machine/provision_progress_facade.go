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

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machineexecutionstep "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/step/machineexecution"
	machineexecutionmodel "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/step/machineexecution/model"
)

func (steps *workflowRuntime) ResolveProvisionProgressReference(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (machineexecutionmodel.ProvisionProgressReferenceResult, error) {
	return steps.resolveProvisionProgressReferenceStep(ctx, machine)
}

func (steps *workflowRuntime) ClearStaleProvisionOperationReference(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	reference *infrastructurev1beta1.ResourceReference,
) error {
	_, err := steps.clearStaleProvisionOperationReferenceStep(ctx, machine, reference)
	return err
}

func (steps *workflowRuntime) DecideProvisionProgress(
	operation *infrastructurev1beta1.TartHostOperation,
) (machineexecutionmodel.ProvisionProgressDecisionResult, error) {
	return machineexecutionstep.DecideProvisionProgress(operation)
}

func (steps *workflowRuntime) PatchProvisionFailureStatus(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	failure machineexecutionmodel.ProvisionProgressFailed,
) error {
	_, err := steps.patchProvisionFailureStatusStep(ctx, machine, failure)
	return err
}
