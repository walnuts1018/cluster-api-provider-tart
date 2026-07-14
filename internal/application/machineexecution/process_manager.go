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
	machineexecutionmodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution/model"
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinelifecycle"
)

func (workflow *Workflow) Reconcile(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	if workflow.steps == nil {
		return fmt.Errorf("reconcile TartMachine execution: StepExecutor is not configured")
	}
	return workflow.reconcileActive(ctx, machineexecutionmodel.ActiveFromAPI(machine))
}

func (workflow *Workflow) reconcileActive(
	ctx context.Context,
	active activeMachine,
) error {
	switch machinelifecycledomain.DecideMachine(active.State) {
	case machinelifecycledomain.CommandObserveProvisionedMachine{}:
		return workflow.reconcileProvisioned(ctx, provisionedMachine(active))
	case machinelifecycledomain.CommandEnsureProvisionReference{}:
		return workflow.reconcileProvisioning(ctx, provisioningMachine(active))
	default:
		return fmt.Errorf("unknown TartMachine command")
	}
}

func (workflow *Workflow) reconcileProvisioning(
	ctx context.Context,
	provisioning provisioningMachine,
) error {
	referenceResult, err := workflow.steps.ensureProvisionReferenceStep(ctx, provisioning)
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

	switch machinelifecycledomain.DecideProvision(provisioning.State) {
	case machinelifecycledomain.CommandStartProvision{}:
		if err := workflow.steps.startProvisionStep(ctx, provisioning); err != nil {
			return err
		}
	case machinelifecycledomain.CommandResumeProvisionOperation{}:
		if err := workflow.steps.resumeProvisionOperationStep(ctx, provisioning); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown TartMachine provision command")
	}

	return workflow.steps.observeNodeHealthStep(ctx, provisioning.Machine)
}

func (workflow *Workflow) reconcileProvisioned(
	ctx context.Context,
	provisioned provisionedMachine,
) error {
	updateResult, err := workflow.steps.reconcileUpdateOperationStep(ctx, provisioned)
	if err != nil {
		return err
	}
	switch updateResult.(type) {
	case machineexecutionmodel.UpdateOperationTerminalHandled:
		return nil
	case machineexecutionmodel.UpdateOperationNeedsNodeHealth:
	default:
		return fmt.Errorf("unknown Update operation result: %T", updateResult)
	}
	return workflow.steps.observeNodeHealthStep(ctx, provisioned.Machine)
}
