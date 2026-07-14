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
	clusterlifecyclemodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/clusterlifecycle/model"
	clusterstatus "github.com/walnuts1018/cluster-api-provider-tart/internal/application/clusterstatus"
	resourcefinalizer "github.com/walnuts1018/cluster-api-provider-tart/internal/application/resourcefinalizer"
	clusterlifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/clusterlifecycle"
)

type FinalizerStep interface {
	Ensure(context.Context, client.Object) (resourcefinalizer.Result, error)
	Release(context.Context, client.Object) (resourcefinalizer.Result, error)
}

type StatusStep interface {
	Reconcile(context.Context, *infrastructurev1beta1.TartCluster) (clusterstatus.Result, error)
}

type CommandHandler struct {
	finalizer FinalizerStep
	status    StatusStep
}

func NewCommandHandler(finalizer FinalizerStep, status StatusStep) *CommandHandler {
	return &CommandHandler{
		finalizer: finalizer,
		status:    status,
	}
}

func (handler *CommandHandler) Handle(
	ctx context.Context,
	cluster *infrastructurev1beta1.TartCluster,
	command clusterlifecycledomain.Command,
) (clusterlifecyclemodel.Result, error) {
	switch command.(type) {
	case clusterlifecycledomain.CommandReconcileActive:
		return handler.reconcileActive(ctx, cluster)
	case clusterlifecycledomain.CommandFinalizeDeleting:
		result, err := handler.finalizer.Release(ctx, cluster)
		return clusterlifecyclemodel.ResultFinalizerReleased{Finalizer: result}, err
	default:
		return nil, fmt.Errorf("unknown TartCluster lifecycle command: %T", command)
	}
}

func (handler *CommandHandler) reconcileActive(
	ctx context.Context,
	cluster *infrastructurev1beta1.TartCluster,
) (clusterlifecyclemodel.Result, error) {
	finalizerResult, err := handler.finalizer.Ensure(ctx, cluster)
	if err != nil {
		return nil, err
	}
	statusResult, err := handler.status.Reconcile(ctx, cluster)
	if err != nil {
		return nil, err
	}
	return clusterlifecyclemodel.ResultActiveReconciled{
		Finalizer: finalizerResult,
		Status:    statusResult,
	}, nil
}
