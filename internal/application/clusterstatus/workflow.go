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
	clusterstatusmodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/clusterstatus/model"
	clusterstatusstep "github.com/walnuts1018/cluster-api-provider-tart/internal/application/clusterstatus/step"
	clusterstatusdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/clusterstatus"
)

type Result = clusterstatusmodel.Result
type ResultSkippedMissingClusterLabel = clusterstatusmodel.ResultSkippedMissingClusterLabel
type ResultSkippedClusterNotFound = clusterstatusmodel.ResultSkippedClusterNotFound
type ResultSkippedPausedCluster = clusterstatusmodel.ResultSkippedPausedCluster
type ResultUnchanged = clusterstatusmodel.ResultUnchanged
type ResultPatched = clusterstatusmodel.ResultPatched

type Workflow struct {
	steps *clusterstatusstep.Executor
}

func NewWorkflow(k8sClient client.Client) *Workflow {
	steps := clusterstatusstep.NewExecutor(k8sClient, StatusWithPlan)
	return &Workflow{steps: steps}
}

func (w *Workflow) Reconcile(ctx context.Context, cluster *infrastructurev1beta1.TartCluster) (Result, error) {
	capiCluster, err := w.steps.ObserveCAPICluster(ctx, cluster)
	if err != nil {
		return nil, err
	}

	decision, err := clusterstatusdomain.Decide(w.steps.ObserveTartCluster(cluster), capiCluster)
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
		return w.steps.ApplyStatus(ctx, cluster, decided.Plan)
	default:
		return nil, fmt.Errorf("unknown cluster status decision %T", decision)
	}
}
