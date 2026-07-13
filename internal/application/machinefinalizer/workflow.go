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

package machinefinalizer

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinefinalizerdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinefinalizer"
)

const TartMachineCleanupFinalizer = "infrastructure.cluster.x-k8s.io/tartmachine-cleanup"

type Result interface {
	isResult()
}

type ResultUnchanged struct{}
type ResultPatched struct{}

func (ResultUnchanged) isResult() {}
func (ResultPatched) isResult()   {}

type Workflow struct {
	client.Client
	Finalizer string
}

func NewWorkflow(k8sClient client.Client) *Workflow {
	return &Workflow{
		Client:    k8sClient,
		Finalizer: TartMachineCleanupFinalizer,
	}
}

func (workflow *Workflow) Ensure(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (Result, error) {
	return workflow.apply(ctx, machine, machinefinalizerdomain.DesiredPresent{})
}

func (workflow *Workflow) Release(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (Result, error) {
	return workflow.apply(ctx, machine, machinefinalizerdomain.DesiredAbsent{})
}

func (workflow *Workflow) Present(machine *infrastructurev1beta1.TartMachine) bool {
	return controllerutil.ContainsFinalizer(machine, workflow.finalizer())
}

func (workflow *Workflow) apply(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	desired machinefinalizerdomain.DesiredState,
) (Result, error) {
	command, err := machinefinalizerdomain.Decide(desired, workflow.observe(machine))
	if err != nil {
		return nil, err
	}
	switch command.(type) {
	case machinefinalizerdomain.CommandAdd:
		return ResultPatched{}, workflow.patch(ctx, machine, controllerutil.AddFinalizer)
	case machinefinalizerdomain.CommandRemove:
		return ResultPatched{}, workflow.patch(ctx, machine, controllerutil.RemoveFinalizer)
	case machinefinalizerdomain.CommandNoop:
		return ResultUnchanged{}, nil
	default:
		return nil, fmt.Errorf("unknown TartMachine finalizer command: %T", command)
	}
}

func (workflow *Workflow) observe(machine *infrastructurev1beta1.TartMachine) machinefinalizerdomain.ObservedState {
	if workflow.Present(machine) {
		return machinefinalizerdomain.ObservedPresent{}
	}
	return machinefinalizerdomain.ObservedAbsent{}
}

func (workflow *Workflow) patch(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	transition func(client.Object, string) bool,
) error {
	original := machine.DeepCopy()
	transition(machine, workflow.finalizer())
	if err := workflow.Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("patch TartMachine cleanup finalizer: %w", err)
	}
	return nil
}

func (workflow *Workflow) finalizer() string {
	if workflow.Finalizer == "" {
		return TartMachineCleanupFinalizer
	}
	return workflow.Finalizer
}
