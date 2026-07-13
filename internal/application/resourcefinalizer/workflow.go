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

package resourcefinalizer

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	resourcefinalizerdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/resourcefinalizer"
)

const (
	TartClusterFinalizer         = "infrastructure.cluster.x-k8s.io/tartcluster"
	TartMachineTemplateFinalizer = "infrastructure.cluster.x-k8s.io/tartmachinetemplate"
	TartMachineCleanupFinalizer  = "infrastructure.cluster.x-k8s.io/tartmachine-cleanup"
)

type Result interface {
	isResult()
}

type ResultUnchanged struct{}
type ResultPatched struct{}

func (ResultUnchanged) isResult() {}
func (ResultPatched) isResult()   {}

type Workflow struct {
	client       client.Client
	name         resourcefinalizerdomain.Name
	resourceName string
}

func NewWorkflow(k8sClient client.Client, finalizer string, resource string) (*Workflow, error) {
	name, err := resourcefinalizerdomain.ParseName(finalizer)
	if err != nil {
		return nil, err
	}
	return &Workflow{
		client:       k8sClient,
		name:         name,
		resourceName: resource,
	}, nil
}

func NewTartClusterWorkflow(k8sClient client.Client) *Workflow {
	return mustWorkflow(k8sClient, TartClusterFinalizer, "TartCluster")
}

func NewTartMachineTemplateWorkflow(k8sClient client.Client) *Workflow {
	return mustWorkflow(k8sClient, TartMachineTemplateFinalizer, "TartMachineTemplate")
}

func NewTartMachineWorkflow(k8sClient client.Client) *Workflow {
	return mustWorkflow(k8sClient, TartMachineCleanupFinalizer, "TartMachine")
}

func mustWorkflow(k8sClient client.Client, finalizer string, resource string) *Workflow {
	workflow, err := NewWorkflow(k8sClient, finalizer, resource)
	if err != nil {
		panic(err)
	}
	return workflow
}

func (workflow *Workflow) Ensure(ctx context.Context, object client.Object) (Result, error) {
	return workflow.apply(ctx, object, resourcefinalizerdomain.DesiredPresent{})
}

func (workflow *Workflow) Release(ctx context.Context, object client.Object) (Result, error) {
	return workflow.apply(ctx, object, resourcefinalizerdomain.DesiredAbsent{})
}

func (workflow *Workflow) Present(object client.Object) bool {
	return controllerutil.ContainsFinalizer(object, workflow.finalizer())
}

func (workflow *Workflow) apply(
	ctx context.Context,
	object client.Object,
	desired resourcefinalizerdomain.DesiredState,
) (Result, error) {
	command, err := resourcefinalizerdomain.Decide(desired, workflow.observe(object))
	if err != nil {
		return nil, err
	}
	switch command.(type) {
	case resourcefinalizerdomain.CommandAdd:
		return ResultPatched{}, workflow.patch(ctx, object, controllerutil.AddFinalizer)
	case resourcefinalizerdomain.CommandRemove:
		return ResultPatched{}, workflow.patch(ctx, object, controllerutil.RemoveFinalizer)
	case resourcefinalizerdomain.CommandNoop:
		return ResultUnchanged{}, nil
	default:
		return nil, fmt.Errorf("unknown resource finalizer command: %T", command)
	}
}

func (workflow *Workflow) observe(object client.Object) resourcefinalizerdomain.ObservedState {
	if workflow.Present(object) {
		return resourcefinalizerdomain.ObservedPresent{}
	}
	return resourcefinalizerdomain.ObservedAbsent{}
}

func (workflow *Workflow) patch(
	ctx context.Context,
	object client.Object,
	transition func(client.Object, string) bool,
) error {
	original := object.DeepCopyObject().(client.Object)
	transition(object, workflow.finalizer())
	if err := workflow.client.Patch(ctx, object, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("patch %s finalizer: %w", workflow.resource(), err)
	}
	return nil
}

func (workflow *Workflow) finalizer() string {
	return workflow.name.String()
}

func (workflow *Workflow) resource() string {
	if workflow.resourceName == "" {
		return "resource"
	}
	return workflow.resourceName
}
