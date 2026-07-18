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

	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/entity/machinelifecycle"
	machineexecutionmodel "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/step/machineexecution/model"
)

type pipelinePorts interface {
	provisionReferencePorts
	provisionProgressPorts
	updateOperationPorts
	nodeHealthPorts
	healthGatePorts
}

type provisionReferencePorts interface {
	EnsureProvisionReference(
		context.Context,
		machineexecutionmodel.ProvisioningMachine,
	) (machineexecutionmodel.ProvisionReferenceResult, error)
	StartProvision(context.Context, machineexecutionmodel.ProvisioningMachine) error
}

type pipeline struct {
	provisionSteps    provisionReferencePorts
	provisionProgress *provisionProgress
	updateOperation   *updateOperation
	nodeHealth        *nodeHealth
}

func newPipeline(steps pipelinePorts) *pipeline {
	healthGate := newHealthGate(steps)
	nodeHealth := newNodeHealth(steps, healthGate)
	return &pipeline{
		provisionSteps:    steps,
		provisionProgress: newProvisionProgress(steps),
		updateOperation:   newUpdateOperation(steps),
		nodeHealth:        nodeHealth,
	}
}

func (handler *pipeline) run(
	ctx context.Context,
	active machineexecutionmodel.ActiveMachine,
	command machinelifecycledomain.MachineCommand,
) error {
	switch command.(type) {
	case machinelifecycledomain.CommandObserveProvisionedMachine:
		return handler.reconcileProvisioned(ctx, machineexecutionmodel.ProvisionedMachine(active))
	case machinelifecycledomain.CommandEnsureProvisionReference:
		return handler.reconcileProvisioning(ctx, machineexecutionmodel.ProvisioningMachine(active))
	default:
		return fmt.Errorf("unknown TartMachine command: %T", command)
	}
}

func (handler *pipeline) reconcileProvisioning(
	ctx context.Context,
	provisioning machineexecutionmodel.ProvisioningMachine,
) error {
	referenceResult, err := handler.provisionSteps.EnsureProvisionReference(ctx, provisioning)
	if err != nil {
		return err
	}
	switch referenceResult.(type) {
	case machineexecutionmodel.ProvisionReferenceBlocked:
		return nil
	case machineexecutionmodel.ProvisionReferenceReady:
	default:
		return fmt.Errorf("unknown Provision reference result: %T", referenceResult)
	}

	command := machinelifecycledomain.DecideProvision(provisioning.State)
	if err := handler.handleProvision(ctx, provisioning, command); err != nil {
		return err
	}
	return handler.nodeHealth.run(ctx, provisioning.Machine)
}

func (handler *pipeline) handleProvision(
	ctx context.Context,
	provisioning machineexecutionmodel.ProvisioningMachine,
	command machinelifecycledomain.ProvisionCommand,
) error {
	switch command.(type) {
	case machinelifecycledomain.CommandStartProvision:
		return handler.provisionSteps.StartProvision(ctx, provisioning)
	case machinelifecycledomain.CommandResumeProvisionOperation:
		return handler.provisionProgress.run(ctx, provisioning.Machine)
	default:
		return fmt.Errorf("unknown TartMachine provision command: %T", command)
	}
}

func (handler *pipeline) reconcileProvisioned(
	ctx context.Context,
	provisioned machineexecutionmodel.ProvisionedMachine,
) error {
	result, err := handler.updateOperation.run(ctx, provisioned)
	if err != nil {
		return err
	}
	switch result.(type) {
	case machineexecutionmodel.UpdateOperationTerminalHandled:
		return nil
	case machineexecutionmodel.UpdateOperationNeedsNodeHealth:
		return handler.nodeHealth.run(ctx, provisioned.Machine)
	default:
		return fmt.Errorf("unknown Update operation result: %T", result)
	}
}
