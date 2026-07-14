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

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinetemplatelifecyclemodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinetemplatelifecycle/model"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinetemplatelifecycle"
)

type Result = machinetemplatelifecyclemodel.Result
type ResultFinalizerEnsured = machinetemplatelifecyclemodel.ResultFinalizerEnsured
type ResultFinalizerReleased = machinetemplatelifecyclemodel.ResultFinalizerReleased

type Workflow struct {
	ports Ports
}

func NewWorkflowWithFinalizer(finalizer FinalizerStep) *Workflow {
	return NewWorkflow(Ports{Finalizer: finalizer})
}

func NewWorkflow(ports Ports) *Workflow {
	return &Workflow{ports: ports}
}

func (workflow *Workflow) Reconcile(
	ctx context.Context,
	template *infrastructurev1beta1.TartMachineTemplate,
) (Result, error) {
	decision, err := domain.Decide(observe(template))
	if err != nil {
		return nil, err
	}
	return workflow.applyDecision(ctx, template, decision)
}

func observe(template *infrastructurev1beta1.TartMachineTemplate) domain.ObservedState {
	if !template.DeletionTimestamp.IsZero() {
		return domain.ObservedDeleting{}
	}
	return domain.ObservedActive{}
}

func (workflow *Workflow) applyDecision(
	ctx context.Context,
	template *infrastructurev1beta1.TartMachineTemplate,
	decision domain.Decision,
) (Result, error) {
	switch decision.(type) {
	case domain.DecisionEnsureFinalizer:
		result, err := workflow.ports.Finalizer.Ensure(ctx, template)
		return ResultFinalizerEnsured{Finalizer: result}, err
	case domain.DecisionReleaseFinalizer:
		result, err := workflow.ports.Finalizer.Release(ctx, template)
		return ResultFinalizerReleased{Finalizer: result}, err
	default:
		return nil, fmt.Errorf("unknown TartMachineTemplate lifecycle decision: %T", decision)
	}
}
