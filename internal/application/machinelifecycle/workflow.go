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

	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinedeletion "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinedeletion"
	resourcefinalizer "github.com/walnuts1018/cluster-api-provider-tart/internal/application/resourcefinalizer"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinelifecycle"
)

type Result interface {
	isResult()
}

type ResultActiveReconciled struct {
	Finalizer resourcefinalizer.Result
}

type ResultDeleteWaiting struct {
	Deletion machinedeletion.Result
}

type ResultFinalizerReleased struct {
	Deletion  machinedeletion.Result
	Finalizer resourcefinalizer.Result
}

type ResultDeletingIgnored struct{}

func (ResultActiveReconciled) isResult()  {}
func (ResultDeleteWaiting) isResult()     {}
func (ResultFinalizerReleased) isResult() {}
func (ResultDeletingIgnored) isResult()   {}

type FinalizerStep interface {
	Ensure(context.Context, client.Object) (resourcefinalizer.Result, error)
	Release(context.Context, client.Object) (resourcefinalizer.Result, error)
	Present(client.Object) bool
}

type ExecutionStep interface {
	Reconcile(context.Context, *infrastructurev1beta1.TartMachine) error
}

type DeletionStep interface {
	Reconcile(context.Context, *infrastructurev1beta1.TartMachine) (machinedeletion.Result, error)
}

type Workflow struct {
	finalizer FinalizerStep
	execution ExecutionStep
	deletion  DeletionStep
}

func NewWorkflowWithSteps(
	finalizer FinalizerStep,
	execution ExecutionStep,
	deletion DeletionStep,
) *Workflow {
	return &Workflow{
		finalizer: finalizer,
		execution: execution,
		deletion:  deletion,
	}
}

func (workflow *Workflow) Reconcile(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (Result, error) {
	if workflow.finalizer == nil {
		return nil, fmt.Errorf("reconcile TartMachine lifecycle: Finalizer is not configured")
	}

	command, err := domain.DecideLifecycle(workflow.observe(machine))
	if err != nil {
		return nil, err
	}
	switch command.(type) {
	case domain.CommandReconcileActive:
		if workflow.execution == nil {
			return nil, fmt.Errorf("reconcile active TartMachine: Execution workflow is not configured")
		}
		finalizerResult, err := workflow.finalizer.Ensure(ctx, machine)
		if err != nil {
			return nil, err
		}
		if err := workflow.execution.Reconcile(ctx, machine); err != nil {
			return nil, err
		}
		return ResultActiveReconciled{Finalizer: finalizerResult}, nil
	case domain.CommandFinalizeDeleting:
		if workflow.deletion == nil {
			return nil, fmt.Errorf("finalize deleting TartMachine: Deletion workflow is not configured")
		}
		deletionResult, err := workflow.deletion.Reconcile(ctx, machine)
		if err != nil {
			return nil, err
		}
		switch deletionResult.(type) {
		case machinedeletion.ResultFinalizerReady:
			finalizerResult, err := workflow.finalizer.Release(ctx, machine)
			return ResultFinalizerReleased{Deletion: deletionResult, Finalizer: finalizerResult}, err
		case machinedeletion.ResultWaiting:
			return ResultDeleteWaiting{Deletion: deletionResult}, nil
		default:
			return nil, fmt.Errorf("unknown TartMachine deletion result: %T", deletionResult)
		}
	case domain.CommandIgnoreDeleting:
		return ResultDeletingIgnored{}, nil
	default:
		return nil, fmt.Errorf("unknown TartMachine lifecycle command: %T", command)
	}
}

func (workflow *Workflow) observe(machine *infrastructurev1beta1.TartMachine) domain.ObservedState {
	if machine.DeletionTimestamp.IsZero() {
		return domain.ObservedActive{}
	}
	return domain.ObservedDeleting{FinalizerPresent: workflow.finalizer.Present(machine)}
}
