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

package clusterstatus

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	clusterstatusdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster/entity/clusterstatus"
	sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"
	sharedworkflow "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/workflow"
)

type Command struct {
	Cluster *infrastructurev1beta1.TartCluster
}

type Event interface{ isEvent() }

type ClusterStatusReconciled struct{ Result Result }

func (ClusterStatusReconciled) isEvent() {}

type Workflow struct {
	client        client.Client
	statusPlanner StatusPlanner
}

func NewWorkflow(k8sClient client.Client) *Workflow {
	return &Workflow{client: k8sClient, statusPlanner: StatusWithPlan}
}

func (w *Workflow) Do(ctx context.Context, command Command) sharedresult.Result[Event, sharedworkflow.Failure] {
	result, err := w.execute(ctx, command.Cluster)
	if err != nil {
		return sharedworkflow.Failed[Event](sharedworkflow.DependencyFailure{Operation: "reconcile cluster status", Detail: err.Error()})
	}
	return sharedworkflow.Succeeded[Event](ClusterStatusReconciled{Result: result})
}

func (w *Workflow) execute(ctx context.Context, cluster *infrastructurev1beta1.TartCluster) (Result, error) {
	capiCluster, err := w.observeCAPICluster(ctx, cluster)
	if err != nil {
		return nil, err
	}

	decision, err := clusterstatusdomain.Decide(w.observeTartCluster(cluster), capiCluster)
	if err != nil {
		return nil, err
	}

	return w.applyDecision(ctx, cluster, decision)
}

func (w *Workflow) applyDecision(
	ctx context.Context,
	cluster *infrastructurev1beta1.TartCluster,
	decision clusterstatusdomain.Decision,
) (Result, error) {
	switch decided := decision.(type) {
	case clusterstatusdomain.DecisionSkipMissingClusterLabel:
		return ResultSkippedMissingClusterLabel{}, nil
	case clusterstatusdomain.DecisionSkipClusterNotFound:
		return ResultSkippedClusterNotFound{ClusterName: decided.ClusterName}, nil
	case clusterstatusdomain.DecisionSkipPausedCluster:
		return ResultSkippedPausedCluster{ClusterName: decided.ClusterName}, nil
	case clusterstatusdomain.DecisionApplyStatus:
		return w.applyStatus(ctx, cluster, decided.Plan)
	default:
		return nil, fmt.Errorf("unknown cluster status decision %T", decision)
	}
}
