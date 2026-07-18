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
	domain "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/entity/machinelifecycle"
	machinedeletion "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/delete_machine"
	machineexecution "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/reconcile_machine"
	sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"
	sharedworkflow "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/workflow"
)

type Command struct {
	Machine *infrastructurev1beta1.TartMachine
}
type Event struct{ Result Result }

type Workflow struct {
	ports Ports
}

func NewWorkflowWithPorts(
	finalizer FinalizerPort,
	execution ExecutionWorkflow,
	deletion DeletionWorkflow,
) *Workflow {
	return NewWorkflow(Ports{
		Finalizer: finalizer,
		Execution: execution,
		Deletion:  deletion,
	})
}

func NewWorkflow(ports Ports) *Workflow {
	return &Workflow{
		ports: ports,
	}
}

func (workflow *Workflow) Do(
	ctx context.Context,
	command Command,
) sharedresult.Result[Event, sharedworkflow.Failure] {
	result, err := workflow.execute(ctx, command.Machine)
	if err != nil {
		return sharedworkflow.Failed[Event](sharedworkflow.DependencyFailure{Operation: "reconcile machine lifecycle", Detail: err.Error()})
	}
	return sharedworkflow.Succeeded[Event](Event{Result: result})
}

func (workflow *Workflow) execute(ctx context.Context, machine *infrastructurev1beta1.TartMachine) (Result, error) {
	if err := workflow.ensureConfigured(); err != nil {
		return nil, err
	}

	command, err := domain.DecideLifecycle(workflow.observe(machine))
	if err != nil {
		return nil, err
	}
	return workflow.applyDecision(ctx, machine, command)
}

func (workflow *Workflow) observe(machine *infrastructurev1beta1.TartMachine) domain.ObservedState {
	if machine.DeletionTimestamp.IsZero() {
		return domain.ObservedActive{}
	}
	return domain.ObservedDeleting{FinalizerPresent: workflow.ports.Finalizer.Present(machine)}
}

func (workflow *Workflow) ensureConfigured() error {
	if workflow.ports.Finalizer == nil {
		return fmt.Errorf("reconcile TartMachine lifecycle: Finalizer port is not configured")
	}
	return nil
}

func (workflow *Workflow) applyDecision(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	command domain.LifecycleCommand,
) (Result, error) {
	switch command.(type) {
	case domain.CommandReconcileActive:
		return workflow.reconcileActive(ctx, machine)
	case domain.CommandFinalizeDeleting:
		return workflow.finalizeDeleting(ctx, machine)
	case domain.CommandIgnoreDeleting:
		return ResultDeletingIgnored{}, nil
	default:
		return nil, fmt.Errorf("unknown TartMachine lifecycle command: %T", command)
	}
}

func (workflow *Workflow) reconcileActive(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (Result, error) {
	if workflow.ports.Execution == nil {
		return nil, fmt.Errorf("reconcile active TartMachine: Execution port is not configured")
	}
	finalizerResult, err := workflow.ports.Finalizer.Ensure(ctx, machine)
	if err != nil {
		return nil, err
	}
	executionOutcome := workflow.ports.Execution.Do(ctx, machineexecution.Command{Machine: machine})
	if failure, failed := executionOutcome.FailureValue().Value(); failed {
		return nil, fmt.Errorf("reconcile machine execution: %s", failure.Message())
	}
	return ResultActiveReconciled{Finalizer: finalizerResult}, nil
}

func (workflow *Workflow) finalizeDeleting(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (Result, error) {
	if workflow.ports.Deletion == nil {
		return nil, fmt.Errorf("finalize deleting TartMachine: Deletion port is not configured")
	}
	deletionOutcome := workflow.ports.Deletion.Do(ctx, machinedeletion.Command{Machine: machine})
	deletionEvent, present := deletionOutcome.Value().Value()
	if !present {
		failure, _ := deletionOutcome.FailureValue().Value()
		return nil, fmt.Errorf("delete machine: %s", failure.Message())
	}
	deletionResult := deletionEvent.Result
	switch deletionResult.(type) {
	case machinedeletion.ResultFinalizerReady:
		finalizerResult, err := workflow.ports.Finalizer.Release(ctx, machine)
		return ResultFinalizerReleased{
			Deletion:  deletionResult,
			Finalizer: finalizerResult,
		}, err
	case machinedeletion.ResultWaiting:
		return ResultDeleteWaiting{Deletion: deletionResult}, nil
	default:
		return nil, fmt.Errorf("unknown TartMachine deletion result: %T", deletionResult)
	}
}
