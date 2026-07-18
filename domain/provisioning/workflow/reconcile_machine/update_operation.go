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

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machineexecutionmodel "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/step/machineexecution/model"
)

type updateOperationPorts interface {
	DecideUpdateOperation(
		context.Context,
		machineexecutionmodel.ProvisionedMachine,
	) (machineexecutionmodel.UpdateOperationDecisionResult, error)
	ApplyUpdateTerminal(
		context.Context,
		*infrastructurev1beta1.TartMachine,
		machineexecutionmodel.UpdateOperationApplyTerminal,
	) error
}

type updateOperation struct {
	steps updateOperationPorts
}

func newUpdateOperation(steps updateOperationPorts) *updateOperation {
	return &updateOperation{steps: steps}
}

func (handler *updateOperation) run(
	ctx context.Context,
	provisioned machineexecutionmodel.ProvisionedMachine,
) (machineexecutionmodel.UpdateOperationStepResult, error) {
	decision, err := handler.steps.DecideUpdateOperation(ctx, provisioned)
	if err != nil {
		return nil, err
	}
	return handler.handleDecision(ctx, provisioned.Machine, decision)
}

func (handler *updateOperation) handleDecision(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	decision machineexecutionmodel.UpdateOperationDecisionResult,
) (machineexecutionmodel.UpdateOperationStepResult, error) {
	switch decision := decision.(type) {
	case machineexecutionmodel.UpdateOperationApplyTerminal:
		if err := handler.steps.ApplyUpdateTerminal(ctx, machine, decision); err != nil {
			return nil, err
		}
		return machineexecutionmodel.UpdateOperationTerminalHandled{}, nil
	case machineexecutionmodel.UpdateOperationRouteNodeHealth:
		return machineexecutionmodel.UpdateOperationNeedsNodeHealth{}, nil
	default:
		return nil, fmt.Errorf("unknown Update Operation decision result: %T", decision)
	}
}
