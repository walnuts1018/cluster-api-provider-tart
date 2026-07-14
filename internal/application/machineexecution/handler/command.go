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

package handler

import (
	"context"
	"fmt"

	machineexecutionmodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution/model"
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinelifecycle"
)

type Steps interface {
	ProvisionReferenceSteps
	ProvisionProgressSteps
	UpdateOperationSteps
	NodeHealthSteps
	HealthGateSteps
}

type ProvisionReferenceSteps interface {
	EnsureProvisionReference(
		context.Context,
		machineexecutionmodel.ProvisioningMachine,
	) (machineexecutionmodel.ProvisionReferenceResult, error)
	StartProvision(context.Context, machineexecutionmodel.ProvisioningMachine) error
}

type CommandHandler struct {
	provisionSteps    ProvisionReferenceSteps
	provisionProgress *ProvisionProgressHandler
	updateOperation   *UpdateOperationHandler
	nodeHealth        *NodeHealthHandler
}

func NewCommandHandler(steps Steps) *CommandHandler {
	healthGate := NewHealthGateHandler(steps)
	nodeHealth := NewNodeHealthHandler(steps, healthGate)
	return &CommandHandler{
		provisionSteps:    steps,
		provisionProgress: NewProvisionProgressHandler(steps),
		updateOperation:   NewUpdateOperationHandler(steps),
		nodeHealth:        nodeHealth,
	}
}

func (handler *CommandHandler) HandleMachine(
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

func (handler *CommandHandler) reconcileProvisioning(
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
	return handler.nodeHealth.Handle(ctx, provisioning.Machine)
}

func (handler *CommandHandler) handleProvision(
	ctx context.Context,
	provisioning machineexecutionmodel.ProvisioningMachine,
	command machinelifecycledomain.ProvisionCommand,
) error {
	switch command.(type) {
	case machinelifecycledomain.CommandStartProvision:
		return handler.provisionSteps.StartProvision(ctx, provisioning)
	case machinelifecycledomain.CommandResumeProvisionOperation:
		return handler.provisionProgress.Handle(ctx, provisioning.Machine)
	default:
		return fmt.Errorf("unknown TartMachine provision command: %T", command)
	}
}

func (handler *CommandHandler) reconcileProvisioned(
	ctx context.Context,
	provisioned machineexecutionmodel.ProvisionedMachine,
) error {
	result, err := handler.updateOperation.Handle(ctx, provisioned)
	if err != nil {
		return err
	}
	switch result.(type) {
	case machineexecutionmodel.UpdateOperationTerminalHandled:
		return nil
	case machineexecutionmodel.UpdateOperationNeedsNodeHealth:
		return handler.nodeHealth.Handle(ctx, provisioned.Machine)
	default:
		return fmt.Errorf("unknown Update operation result: %T", result)
	}
}
