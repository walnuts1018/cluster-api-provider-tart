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

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	domain "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster/entity/clusterlifecycle"
	clusterstatus "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster/workflow/reconcile_cluster_status"
	sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"
	sharedworkflow "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/workflow"
)

type Command struct {
	Cluster *infrastructurev1beta1.TartCluster
}

type Event interface{ isEvent() }

type ClusterReconciled struct{ Result Result }

func (ClusterReconciled) isEvent() {}

type Workflow struct {
	ports Ports
}

func NewWorkflowWithPorts(finalizer FinalizerPort, status StatusWorkflow) *Workflow {
	return NewWorkflow(Ports{Finalizer: finalizer, Status: status})
}

func NewWorkflow(ports Ports) *Workflow {
	return &Workflow{ports: ports}
}

func (workflow *Workflow) Do(
	ctx context.Context,
	command Command,
) sharedresult.Result[Event, sharedworkflow.Failure] {
	result, err := workflow.execute(ctx, command.Cluster)
	if err != nil {
		return sharedworkflow.Failed[Event](sharedworkflow.DependencyFailure{Operation: "reconcile cluster", Detail: err.Error()})
	}
	return sharedworkflow.Succeeded[Event](ClusterReconciled{Result: result})
}

func (workflow *Workflow) execute(ctx context.Context, cluster *infrastructurev1beta1.TartCluster) (Result, error) {
	command, err := domain.Decide(observe(cluster))
	if err != nil {
		return nil, err
	}
	return workflow.applyDecision(ctx, cluster, command)
}

func observe(cluster *infrastructurev1beta1.TartCluster) domain.ObservedState {
	if !cluster.DeletionTimestamp.IsZero() {
		return domain.ObservedDeleting{}
	}
	return domain.ObservedActive{}
}

func (workflow *Workflow) applyDecision(
	ctx context.Context,
	cluster *infrastructurev1beta1.TartCluster,
	command domain.Command,
) (Result, error) {
	switch command.(type) {
	case domain.CommandReconcileActive:
		return workflow.reconcileActive(ctx, cluster)
	case domain.CommandFinalizeDeleting:
		result, err := workflow.ports.Finalizer.Release(ctx, cluster)
		return ResultFinalizerReleased{Finalizer: result}, err
	default:
		return nil, fmt.Errorf("unknown TartCluster lifecycle command: %T", command)
	}
}

func (workflow *Workflow) reconcileActive(
	ctx context.Context,
	cluster *infrastructurev1beta1.TartCluster,
) (Result, error) {
	finalizerResult, err := workflow.ports.Finalizer.Ensure(ctx, cluster)
	if err != nil {
		return nil, err
	}
	statusOutcome := workflow.ports.Status.Do(ctx, clusterstatus.Command{Cluster: cluster})
	statusEvent, present := statusOutcome.Value().Value()
	if !present {
		failure, _ := statusOutcome.FailureValue().Value()
		return nil, fmt.Errorf("reconcile cluster status: %s", failure.Message())
	}
	statusResult := statusEvent.(clusterstatus.ClusterStatusReconciled).Result
	return ResultActiveReconciled{
		Finalizer: finalizerResult,
		Status:    statusResult,
	}, nil
}
