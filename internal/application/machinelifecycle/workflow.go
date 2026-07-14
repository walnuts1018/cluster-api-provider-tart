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

package machinelifecycle

import (
	"context"
	"fmt"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinelifecyclehandler "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinelifecycle/handler"
	machinelifecyclemodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinelifecycle/model"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinelifecycle"
)

type Result = machinelifecyclemodel.Result
type ResultActiveReconciled = machinelifecyclemodel.ResultActiveReconciled
type ResultDeleteWaiting = machinelifecyclemodel.ResultDeleteWaiting
type ResultFinalizerReleased = machinelifecyclemodel.ResultFinalizerReleased
type ResultDeletingIgnored = machinelifecyclemodel.ResultDeletingIgnored

type FinalizerStep = machinelifecyclehandler.FinalizerStep
type ExecutionStep = machinelifecyclehandler.ExecutionStep
type DeletionStep = machinelifecyclehandler.DeletionStep

type Workflow struct {
	commands *machinelifecyclehandler.CommandHandler
}

func NewWorkflowWithSteps(
	finalizer FinalizerStep,
	execution ExecutionStep,
	deletion DeletionStep,
) *Workflow {
	return &Workflow{
		commands: machinelifecyclehandler.NewCommandHandler(finalizer, execution, deletion),
	}
}

func (workflow *Workflow) Reconcile(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (Result, error) {
	if workflow.commands == nil {
		return nil, fmt.Errorf("reconcile TartMachine lifecycle: CommandHandler is not configured")
	}
	if err := workflow.commands.EnsureConfigured(); err != nil {
		return nil, err
	}

	command, err := domain.DecideLifecycle(workflow.observe(machine))
	if err != nil {
		return nil, err
	}
	return workflow.commands.Handle(ctx, machine, command)
}

func (workflow *Workflow) observe(machine *infrastructurev1beta1.TartMachine) domain.ObservedState {
	if machine.DeletionTimestamp.IsZero() {
		return domain.ObservedActive{}
	}
	return domain.ObservedDeleting{FinalizerPresent: workflow.commands.FinalizerPresent(machine)}
}
