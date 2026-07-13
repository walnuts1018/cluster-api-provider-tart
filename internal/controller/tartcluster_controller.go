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

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	clusterstatus "github.com/walnuts1018/cluster-api-provider-tart/internal/application/clusterstatus"
	resourcefinalizer "github.com/walnuts1018/cluster-api-provider-tart/internal/application/resourcefinalizer"
)

// TartClusterReconciler reconciles a TartCluster object
type TartClusterReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Finalizer *resourcefinalizer.Workflow
	Status    *clusterstatus.Workflow
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *TartClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, span := telemetry.Tracer.Start(ctx, "TartCluster.Reconcile")
	span.SetAttributes(
		attribute.String("kubernetes.resource.name", req.Name),
		attribute.String("kubernetes.resource.namespace", req.Namespace),
	)
	defer span.End()

	var cluster infrastructurev1beta1.TartCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if !cluster.DeletionTimestamp.IsZero() {
		if _, err := r.finalizerWorkflow().Release(ctx, &cluster); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if _, err := r.finalizerWorkflow().Ensure(ctx, &cluster); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileNormal(ctx, &cluster); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *TartClusterReconciler) reconcileNormal(ctx context.Context, cluster *infrastructurev1beta1.TartCluster) error {
	log := logf.FromContext(ctx)
	result, err := r.statusWorkflow().Reconcile(ctx, cluster)
	if err != nil {
		return err
	}

	switch observed := result.(type) {
	case clusterstatus.ResultSkippedMissingClusterLabel:
		log.V(4).Info("TartCluster missing cluster label, skipping", "cluster", cluster.Name)
	case clusterstatus.ResultSkippedClusterNotFound:
		log.V(4).Info("Cluster not found, skipping reconciliation", "cluster", observed.ClusterName)
	case clusterstatus.ResultSkippedPausedCluster:
		log.V(4).Info("Cluster is paused, skipping reconciliation", "cluster", observed.ClusterName)
	}

	return nil
}

func (r *TartClusterReconciler) finalizerWorkflow() *resourcefinalizer.Workflow {
	if r.Finalizer != nil {
		return r.Finalizer
	}
	return resourcefinalizer.NewTartClusterWorkflow(r.Client)
}

func (r *TartClusterReconciler) statusWorkflow() *clusterstatus.Workflow {
	if r.Status != nil {
		return r.Status
	}
	return clusterstatus.NewWorkflow(r.Client)
}

// SetupWithManager sets up the controller with the Manager.
func (r *TartClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1beta1.TartCluster{}).
		Watches(
			&clusterv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(r.clusterToTartCluster),
		).
		Named("tartcluster").
		Complete(r)
}

// clusterToTartCluster maps CAPI Cluster events to TartCluster reconcile requests.
func (r *TartClusterReconciler) clusterToTartCluster(ctx context.Context, obj client.Object) []reconcile.Request {
	cluster, ok := obj.(*clusterv1.Cluster)
	if !ok {
		return nil
	}

	// Find TartCluster by label
	labelMap := map[string]string{
		clusterv1.ClusterNameLabel: cluster.Name,
	}
	var tartClusterList infrastructurev1beta1.TartClusterList
	if err := r.List(ctx, &tartClusterList, client.MatchingLabels(labelMap)); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, 0, len(tartClusterList.Items))
	for _, tc := range tartClusterList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: tc.Namespace,
				Name:      tc.Name,
			},
		})
	}
	return requests
}
