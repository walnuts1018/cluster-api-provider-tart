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
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinelifecycle"
)

func (workflow *Workflow) Reconcile(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	return workflow.reconcileActive(ctx, activeMachine{
		Machine: machine,
		State:   machineState(machine),
	})
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
	shouldContinue, err := workflow.ensureProvisionReferenceStep(ctx, provisioning)
	if err != nil {
		return err
	}
	if !shouldContinue {
		return nil
	}

	switch machinelifecycledomain.DecideProvision(provisioning.State) {
	case machinelifecycledomain.CommandStartProvision{}:
		if err := workflow.startProvisionStep(ctx, provisioning); err != nil {
			return err
		}
	case machinelifecycledomain.CommandResumeProvisionOperation{}:
		if err := workflow.resumeProvisionOperationStep(ctx, provisioning); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown TartMachine provision command")
	}

	return workflow.observeNodeHealthStep(ctx, provisioning.Machine)
}

func (workflow *Workflow) reconcileProvisioned(
	ctx context.Context,
	provisioned provisionedMachine,
) error {
	updateHandled, err := workflow.reconcileUpdateOperationStep(ctx, provisioned)
	if err != nil {
		return err
	}
	if updateHandled {
		return nil
	}
	return workflow.observeNodeHealthStep(ctx, provisioned.Machine)
}
