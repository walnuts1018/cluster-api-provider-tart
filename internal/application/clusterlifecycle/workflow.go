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
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	clusterstatus "github.com/walnuts1018/cluster-api-provider-tart/internal/application/clusterstatus"
	resourcefinalizer "github.com/walnuts1018/cluster-api-provider-tart/internal/application/resourcefinalizer"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/clusterlifecycle"
)

type Result interface {
	isResult()
}

type ResultActiveReconciled struct {
	Finalizer resourcefinalizer.Result
	Status    clusterstatus.Result
}

type ResultFinalizerReleased struct {
	Finalizer resourcefinalizer.Result
}

func (ResultActiveReconciled) isResult()  {}
func (ResultFinalizerReleased) isResult() {}

type Workflow struct {
	finalizer *resourcefinalizer.Workflow
	status    *clusterstatus.Workflow
}

func NewWorkflow(k8sClient client.Client) *Workflow {
	return NewWorkflowWithSteps(
		resourcefinalizer.NewTartClusterWorkflow(k8sClient),
		clusterstatus.NewWorkflow(k8sClient),
	)
}

func NewWorkflowWithSteps(finalizer *resourcefinalizer.Workflow, status *clusterstatus.Workflow) *Workflow {
	return &Workflow{
		finalizer: finalizer,
		status:    status,
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

	switch command.(type) {
	case domain.CommandReconcileActive:
		return workflow.reconcileActive(ctx, cluster)
	case domain.CommandFinalizeDeleting:
		result, err := workflow.finalizer.Release(ctx, cluster)
		return ResultFinalizerReleased{Finalizer: result}, err
	default:
		return nil, fmt.Errorf("unknown TartCluster lifecycle command: %T", command)
	}
}

func (workflow *Workflow) reconcileActive(
	ctx context.Context,
	cluster *infrastructurev1beta1.TartCluster,
) (Result, error) {
	finalizerResult, err := workflow.finalizer.Ensure(ctx, cluster)
	if err != nil {
		return nil, err
	}

	statusResult, err := workflow.status.Reconcile(ctx, cluster)
	if err != nil {
		return nil, err
	}

	return ResultActiveReconciled{
		Finalizer: finalizerResult,
		Status:    statusResult,
	}, nil
}

func observe(cluster *infrastructurev1beta1.TartCluster) domain.ObservedState {
	if !cluster.DeletionTimestamp.IsZero() {
		return domain.ObservedDeleting{}
	}
	return domain.ObservedActive{}
}
