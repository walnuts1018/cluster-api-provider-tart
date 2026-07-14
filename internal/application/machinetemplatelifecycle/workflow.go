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

package machinetemplatelifecycle

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	resourcefinalizer "github.com/walnuts1018/cluster-api-provider-tart/internal/application/resourcefinalizer"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinetemplatelifecycle"
)

type Result interface {
	isResult()
}

type ResultFinalizerEnsured struct {
	Finalizer resourcefinalizer.Result
}

type ResultFinalizerReleased struct {
	Finalizer resourcefinalizer.Result
}

func (ResultFinalizerEnsured) isResult()  {}
func (ResultFinalizerReleased) isResult() {}

type FinalizerStep interface {
	Ensure(context.Context, client.Object) (resourcefinalizer.Result, error)
	Release(context.Context, client.Object) (resourcefinalizer.Result, error)
}

type Workflow struct {
	finalizer FinalizerStep
}

func NewWorkflowWithFinalizer(finalizer FinalizerStep) *Workflow {
	return &Workflow{
		finalizer: finalizer,
	}
}

func (workflow *Workflow) Reconcile(
	ctx context.Context,
	template *infrastructurev1beta1.TartMachineTemplate,
) (Result, error) {
	decision, err := domain.Decide(observe(template))
	if err != nil {
		return nil, err
	}

	switch decision.(type) {
	case domain.DecisionEnsureFinalizer:
		result, err := workflow.finalizer.Ensure(ctx, template)
		return ResultFinalizerEnsured{Finalizer: result}, err
	case domain.DecisionReleaseFinalizer:
		result, err := workflow.finalizer.Release(ctx, template)
		return ResultFinalizerReleased{Finalizer: result}, err
	default:
		return nil, fmt.Errorf("unknown TartMachineTemplate lifecycle decision: %T", decision)
	}
}

func observe(template *infrastructurev1beta1.TartMachineTemplate) domain.ObservedState {
	if !template.DeletionTimestamp.IsZero() {
		return domain.ObservedDeleting{}
	}
	return domain.ObservedActive{}
}
