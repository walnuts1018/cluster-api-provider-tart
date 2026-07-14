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
	machineexecutionmodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution/model"
)

func (steps *StepExecutor) EnsureProvisionReference(
	ctx context.Context,
	provisioning machineexecutionmodel.ProvisioningMachine,
) (machineexecutionmodel.ProvisionReferenceResult, error) {
	return steps.ensureProvisionReferenceStep(ctx, provisioning)
}

func (steps *StepExecutor) StartProvision(
	ctx context.Context,
	provisioning machineexecutionmodel.ProvisioningMachine,
) error {
	return steps.startProvisionStep(ctx, provisioning)
}

func (steps *StepExecutor) ResumeProvisionOperation(
	ctx context.Context,
	provisioning machineexecutionmodel.ProvisioningMachine,
) error {
	return steps.resumeProvisionOperationStep(ctx, provisioning)
}

func (steps *StepExecutor) DecideUpdateOperation(
	ctx context.Context,
	provisioned machineexecutionmodel.ProvisionedMachine,
) (machineexecutionmodel.UpdateOperationDecisionResult, error) {
	return steps.decideUpdateOperationStep(ctx, provisioned)
}

func (steps *StepExecutor) ApplyUpdateTerminal(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	decision machineexecutionmodel.UpdateOperationApplyTerminal,
) error {
	return steps.applyUpdateTerminalStep(ctx, machine, decision.Operation, decision.Outcome)
}

func (steps *StepExecutor) ObserveNodeHealth(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	return steps.observeNodeHealthStep(ctx, machine)
}
