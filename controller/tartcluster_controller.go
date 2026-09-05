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

package controller

import (
	"context"
	"time"
	"uuid"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

// TartClusterReconciler reconciles a TartCluster object.
type TartClusterReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartclusters,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartclusters/status,verbs=get;update;patch

func (r *TartClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cluster infrav1alpha1.TartCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if isPaused(&cluster) {
		return ctrl.Result{}, nil
	}
	if !cluster.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// TartCluster.spec.id must be generated exactly once, after the concrete
	// (non-dry-run) Resource is created, and must never be regenerated. Secret bundle
	// generation, Host claim and provisioning must not start before it is set.
	if cluster.Spec.ID == "" {
		original := cluster.DeepCopy()
		cluster.Spec.ID = uuid.NewV4().String()
		if err := r.Patch(ctx, &cluster, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	// TODO: control plane endpoint観測、failure domain反映、secret bundle世代管理は
	// controlplaneパッケージの実装後にここへ接続する。
	original := cluster.DeepCopy()
	setNotImplemented(&cluster.Status.Conditions, infrav1alpha1.TartClusterReadyCondition, cluster.Generation)
	cluster.Status.ObservedGeneration = cluster.Generation
	if err := r.Status().Patch(ctx, &cluster, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *TartClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1alpha1.TartCluster{}).
		Named("tartcluster").
		Complete(r)
}
