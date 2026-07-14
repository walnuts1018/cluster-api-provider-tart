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

	"sigs.k8s.io/controller-runtime/pkg/client"

	resourcefinalizermodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/resourcefinalizer/model"
	resourcefinalizerstep "github.com/walnuts1018/cluster-api-provider-tart/internal/application/resourcefinalizer/step"
	resourcefinalizerdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/resourcefinalizer"
)

const (
	TartClusterFinalizer         = "infrastructure.cluster.x-k8s.io/tartcluster"
	TartMachineTemplateFinalizer = "infrastructure.cluster.x-k8s.io/tartmachinetemplate"
	TartMachineCleanupFinalizer  = "infrastructure.cluster.x-k8s.io/tartmachine-cleanup"
)

type Result = resourcefinalizermodel.Result
type ResultUnchanged = resourcefinalizermodel.ResultUnchanged
type ResultPatched = resourcefinalizermodel.ResultPatched

type Workflow struct {
	steps *resourcefinalizerstep.Executor
}

func NewWorkflow(k8sClient client.Client, finalizer string, resource string) (*Workflow, error) {
	name, err := resourcefinalizerdomain.ParseName(finalizer)
	if err != nil {
		return nil, err
	}
	return &Workflow{
		steps: resourcefinalizerstep.NewExecutor(k8sClient, name, resource),
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
	return workflow.steps.Apply(ctx, object, resourcefinalizerdomain.DesiredPresent{})
}

func (workflow *Workflow) Release(ctx context.Context, object client.Object) (Result, error) {
	return workflow.steps.Apply(ctx, object, resourcefinalizerdomain.DesiredAbsent{})
}

func (workflow *Workflow) Present(object client.Object) bool {
	return workflow.steps.Present(object)
}
