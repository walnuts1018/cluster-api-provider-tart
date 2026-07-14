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

package clusterlifecycle

import (
	"context"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	clusterlifecyclehandler "github.com/walnuts1018/cluster-api-provider-tart/internal/application/clusterlifecycle/handler"
	clusterlifecyclemodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/clusterlifecycle/model"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/clusterlifecycle"
)

type Result = clusterlifecyclemodel.Result
type ResultActiveReconciled = clusterlifecyclemodel.ResultActiveReconciled
type ResultFinalizerReleased = clusterlifecyclemodel.ResultFinalizerReleased

type FinalizerStep = clusterlifecyclehandler.FinalizerStep
type StatusStep = clusterlifecyclehandler.StatusStep

type Workflow struct {
	commands *clusterlifecyclehandler.CommandHandler
}

func NewWorkflowWithSteps(finalizer FinalizerStep, status StatusStep) *Workflow {
	return &Workflow{
		commands: clusterlifecyclehandler.NewCommandHandler(finalizer, status),
	}
}

func (workflow *Workflow) Reconcile(
	ctx context.Context,
	cluster *infrastructurev1beta1.TartCluster,
) (Result, error) {
	command, err := domain.Decide(observe(cluster))
	if err != nil {
		return nil, err
	}
	return workflow.commands.Handle(ctx, cluster, command)
}

func observe(cluster *infrastructurev1beta1.TartCluster) domain.ObservedState {
	if !cluster.DeletionTimestamp.IsZero() {
		return domain.ObservedDeleting{}
	}
	return domain.ObservedActive{}
}
