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

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinetemplatelifecyclehandler "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinetemplatelifecycle/handler"
	machinetemplatelifecyclemodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinetemplatelifecycle/model"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinetemplatelifecycle"
)

type Result = machinetemplatelifecyclemodel.Result
type ResultFinalizerEnsured = machinetemplatelifecyclemodel.ResultFinalizerEnsured
type ResultFinalizerReleased = machinetemplatelifecyclemodel.ResultFinalizerReleased

type FinalizerStep = machinetemplatelifecyclehandler.FinalizerStep

type Workflow struct {
	decisions *machinetemplatelifecyclehandler.DecisionHandler
}

func NewWorkflowWithFinalizer(finalizer FinalizerStep) *Workflow {
	return &Workflow{
		decisions: machinetemplatelifecyclehandler.NewDecisionHandler(finalizer),
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
	return workflow.decisions.Handle(ctx, template, decision)
}

func observe(template *infrastructurev1beta1.TartMachineTemplate) domain.ObservedState {
	if !template.DeletionTimestamp.IsZero() {
		return domain.ObservedDeleting{}
	}
	return domain.ObservedActive{}
}
