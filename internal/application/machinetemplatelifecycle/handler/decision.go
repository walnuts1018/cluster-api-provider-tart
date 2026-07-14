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
	machinetemplatelifecyclemodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinetemplatelifecycle/model"
	resourcefinalizer "github.com/walnuts1018/cluster-api-provider-tart/internal/application/resourcefinalizer"
	machinetemplatelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinetemplatelifecycle"
)

type FinalizerStep interface {
	Ensure(context.Context, client.Object) (resourcefinalizer.Result, error)
	Release(context.Context, client.Object) (resourcefinalizer.Result, error)
}

type DecisionHandler struct {
	finalizer FinalizerStep
}

func NewDecisionHandler(finalizer FinalizerStep) *DecisionHandler {
	return &DecisionHandler{finalizer: finalizer}
}

func (handler *DecisionHandler) Handle(
	ctx context.Context,
	template *infrastructurev1beta1.TartMachineTemplate,
	decision machinetemplatelifecycledomain.Decision,
) (machinetemplatelifecyclemodel.Result, error) {
	switch decision.(type) {
	case machinetemplatelifecycledomain.DecisionEnsureFinalizer:
		result, err := handler.finalizer.Ensure(ctx, template)
		return machinetemplatelifecyclemodel.ResultFinalizerEnsured{Finalizer: result}, err
	case machinetemplatelifecycledomain.DecisionReleaseFinalizer:
		result, err := handler.finalizer.Release(ctx, template)
		return machinetemplatelifecyclemodel.ResultFinalizerReleased{Finalizer: result}, err
	default:
		return nil, fmt.Errorf("unknown TartMachineTemplate lifecycle decision: %T", decision)
	}
}
