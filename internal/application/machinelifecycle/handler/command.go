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

	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinedeletion "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinedeletion"
	machinelifecyclemodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinelifecycle/model"
	resourcefinalizer "github.com/walnuts1018/cluster-api-provider-tart/internal/application/resourcefinalizer"
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinelifecycle"
)

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

type CommandHandler struct {
	finalizer FinalizerStep
	execution ExecutionStep
	deletion  DeletionStep
}

func NewCommandHandler(
	finalizer FinalizerStep,
	execution ExecutionStep,
	deletion DeletionStep,
) *CommandHandler {
	return &CommandHandler{
		finalizer: finalizer,
		execution: execution,
		deletion:  deletion,
	}
}

func (handler *CommandHandler) EnsureConfigured() error {
	if handler.finalizer == nil {
		return fmt.Errorf("reconcile TartMachine lifecycle: Finalizer step is not configured")
	}
	return nil
}

func (handler *CommandHandler) Handle(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	command machinelifecycledomain.LifecycleCommand,
) (machinelifecyclemodel.Result, error) {
	switch command.(type) {
	case machinelifecycledomain.CommandReconcileActive:
		return handler.reconcileActive(ctx, machine)
	case machinelifecycledomain.CommandFinalizeDeleting:
		return handler.finalizeDeleting(ctx, machine)
	case machinelifecycledomain.CommandIgnoreDeleting:
		return machinelifecyclemodel.ResultDeletingIgnored{}, nil
	default:
		return nil, fmt.Errorf("unknown TartMachine lifecycle command: %T", command)
	}
}

func (handler *CommandHandler) FinalizerPresent(object client.Object) bool {
	return handler.finalizer.Present(object)
}

func (handler *CommandHandler) reconcileActive(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (machinelifecyclemodel.Result, error) {
	if handler.execution == nil {
		return nil, fmt.Errorf("reconcile active TartMachine: Execution step is not configured")
	}
	finalizerResult, err := handler.finalizer.Ensure(ctx, machine)
	if err != nil {
		return nil, err
	}
	if err := handler.execution.Reconcile(ctx, machine); err != nil {
		return nil, err
	}
	return machinelifecyclemodel.ResultActiveReconciled{Finalizer: finalizerResult}, nil
}

func (handler *CommandHandler) finalizeDeleting(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (machinelifecyclemodel.Result, error) {
	if handler.deletion == nil {
		return nil, fmt.Errorf("finalize deleting TartMachine: Deletion step is not configured")
	}
	deletionResult, err := handler.deletion.Reconcile(ctx, machine)
	if err != nil {
		return nil, err
	}
	switch deletionResult.(type) {
	case machinedeletion.ResultFinalizerReady:
		finalizerResult, err := handler.finalizer.Release(ctx, machine)
		return machinelifecyclemodel.ResultFinalizerReleased{
			Deletion:  deletionResult,
			Finalizer: finalizerResult,
		}, err
	case machinedeletion.ResultWaiting:
		return machinelifecyclemodel.ResultDeleteWaiting{Deletion: deletionResult}, nil
	default:
		return nil, fmt.Errorf("unknown TartMachine deletion result: %T", deletionResult)
	}
}
