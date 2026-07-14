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

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	clusterstatusmodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/clusterstatus/model"
	clusterstatusstep "github.com/walnuts1018/cluster-api-provider-tart/internal/application/clusterstatus/step"
	clusterstatusdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/clusterstatus"
)

type DecisionHandler struct {
	steps *clusterstatusstep.Executor
}

func NewDecisionHandler(steps *clusterstatusstep.Executor) *DecisionHandler {
	return &DecisionHandler{steps: steps}
}

func (handler *DecisionHandler) Handle(
	ctx context.Context,
	cluster *infrastructurev1beta1.TartCluster,
	decision clusterstatusdomain.Decision,
) (clusterstatusmodel.Result, error) {
	switch decided := decision.(type) {
	case clusterstatusdomain.DecisionSkipMissingClusterLabel:
		return clusterstatusmodel.ResultSkippedMissingClusterLabel{}, nil
	case clusterstatusdomain.DecisionSkipClusterNotFound:
		return clusterstatusmodel.ResultSkippedClusterNotFound{ClusterName: decided.ClusterName}, nil
	case clusterstatusdomain.DecisionSkipPausedCluster:
		return clusterstatusmodel.ResultSkippedPausedCluster{ClusterName: decided.ClusterName}, nil
	case clusterstatusdomain.DecisionApplyStatus:
		return handler.steps.ApplyStatus(ctx, cluster, decided.Plan)
	default:
		return nil, fmt.Errorf("unknown cluster status decision %T", decision)
	}
}
