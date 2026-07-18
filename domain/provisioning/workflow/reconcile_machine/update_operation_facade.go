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
	machineexecutionmodel "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/step/machineexecution/model"
)

func (steps *workflowRuntime) DecideUpdateOperation(
	ctx context.Context,
	provisioned machineexecutionmodel.ProvisionedMachine,
) (machineexecutionmodel.UpdateOperationDecisionResult, error) {
	return steps.decideUpdateOperationStep(ctx, provisioned)
}

func (steps *workflowRuntime) ApplyUpdateTerminal(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	decision machineexecutionmodel.UpdateOperationApplyTerminal,
) error {
	return steps.applyUpdateTerminalStep(ctx, machine, decision.Operation, decision.Outcome)
}
