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
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	clusterstatusdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/clusterstatus"
)

type Result interface {
	result()
}

type ResultSkippedMissingClusterLabel struct{}

func (ResultSkippedMissingClusterLabel) result() {}

type ResultSkippedClusterNotFound struct {
	ClusterName string
}

func (ResultSkippedClusterNotFound) result() {}

type ResultSkippedPausedCluster struct {
	ClusterName string
}

func (ResultSkippedPausedCluster) result() {}

type ResultUnchanged struct{}

func (ResultUnchanged) result() {}

type ResultPatched struct{}

func (ResultPatched) result() {}

type Workflow struct {
	client client.Client
}

func NewWorkflow(k8sClient client.Client) *Workflow {
	return &Workflow{client: k8sClient}
}

func (w *Workflow) Reconcile(ctx context.Context, cluster *infrastructurev1beta1.TartCluster) (Result, error) {
	capiCluster, err := w.observeCAPICluster(ctx, cluster)
	if err != nil {
		return nil, err
	}

	decision, err := clusterstatusdomain.Decide(observeTartCluster(cluster), capiCluster)
	if err != nil {
		return nil, err
	}

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

func (w *Workflow) observeCAPICluster(
	ctx context.Context,
	cluster *infrastructurev1beta1.TartCluster,
) (clusterstatusdomain.CAPICluster, error) {
	clusterName, ok := cluster.Labels[clusterv1.ClusterNameLabel]
	if !ok {
		return clusterstatusdomain.MissingClusterLabel{}, nil
	}

	var capiCluster clusterv1.Cluster
	if err := w.client.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: clusterName}, &capiCluster); err != nil {
		if apierrors.IsNotFound(err) {
			return clusterstatusdomain.ClusterNotFound{Name: clusterName}, nil
		}
		return nil, fmt.Errorf("failed to get Cluster: %w", err)
	}

	observed := clusterstatusdomain.ObservePause(clusterstatusdomain.PauseObservation{
		SpecPaused:      capiCluster.Spec.Paused != nil && *capiCluster.Spec.Paused,
		PausedAnnotated: hasPausedAnnotation(&capiCluster),
	})
	switch observed.(type) {
	case clusterstatusdomain.PausedCluster:
		return clusterstatusdomain.PausedCluster{Name: clusterName}, nil
	case clusterstatusdomain.ActiveCluster:
		return clusterstatusdomain.ActiveCluster{Name: clusterName}, nil
	default:
		return nil, fmt.Errorf("unknown pause observation %T", observed)
	}
}

func observeTartCluster(cluster *infrastructurev1beta1.TartCluster) clusterstatusdomain.TartCluster {
	return clusterstatusdomain.TartCluster{
		Generation:        cluster.Generation,
		ControlPlaneReady: apimeta.IsStatusConditionTrue(cluster.Status.Conditions, ControlPlaneReadyCondition),
	}
}

func hasPausedAnnotation(cluster *clusterv1.Cluster) bool {
	_, ok := cluster.Annotations[clusterv1.PausedAnnotation]
	return ok
}

func (w *Workflow) applyStatus(
	ctx context.Context,
	cluster *infrastructurev1beta1.TartCluster,
	plan clusterstatusdomain.StatusPlan,
) (Result, error) {
	original := cluster.DeepCopy()
	cluster.Status = StatusWithPlan(cluster, plan)
	if reflect.DeepEqual(original.Status, cluster.Status) {
		return ResultUnchanged{}, nil
	}
	if err := w.client.Status().Patch(ctx, cluster, client.MergeFrom(original)); err != nil {
		return nil, err
	}
	return ResultPatched{}, nil
}
