/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"reflect"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TartClusterReconciler reconciles a TartCluster object
type TartClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const tartClusterFinalizer = "infrastructure.cluster.x-k8s.io/tartcluster"

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

	var cluster infrastructurev1alpha1.TartCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !cluster.DeletionTimestamp.IsZero() {
		if err := r.reconcileDelete(ctx, &cluster); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if err := r.ensureFinalizer(ctx, &cluster); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileNormal(ctx, &cluster); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *TartClusterReconciler) reconcileNormal(ctx context.Context, cluster *infrastructurev1alpha1.TartCluster) error {
	log := logf.FromContext(ctx)
	clusterName, ok := cluster.Labels[clusterv1.ClusterNameLabel]
	if !ok {
		log.V(4).Info("TartCluster missing cluster label, skipping", "cluster", cluster.Name)
		return nil
	}

	// Fetch the corresponding CAPI Cluster to check control plane status
	var capiCluster clusterv1.Cluster
	if err := r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: clusterName}, &capiCluster); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to get Cluster: %w", err)
		}
		log.V(4).Info("Cluster not found, skipping reconciliation", "cluster", cluster.Name)
		return nil
	}

	if isClusterPaused(&capiCluster) {
		log.V(4).Info("Cluster is paused, skipping reconciliation", "cluster", clusterName)
		return nil
	}

	original := cluster.DeepCopy()

	// Update status based on cluster state
	cluster.Status.ObservedGeneration = cluster.Generation

	// Mark infrastructure as provisioned when the cluster exists
	cluster.Status.Initialization.Bound = true
	cluster.Status.Initialization.Provisioned = true

	// Check if control plane is ready
	cluster.Status.Initialization.ControlPlaneReady = apimeta.IsStatusConditionTrue(cluster.Status.Conditions, "ControlPlaneReady")
	cluster.Status.Ready = apimeta.IsStatusConditionTrue(cluster.Status.Conditions, "ControlPlaneReady")

	// Set conditions
	apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               "InfrastructureProvisioned",
		Status:             metav1.ConditionTrue,
		Reason:             "Provisioned",
		Message:            "TartCluster infrastructure is provisioned",
		ObservedGeneration: cluster.Generation,
	})

	if !apimeta.IsStatusConditionTrue(cluster.Status.Conditions, "ControlPlaneReady") {
		apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:               "ControlPlaneReady",
			Status:             metav1.ConditionFalse,
			Reason:             "ControlPlaneNotReady",
			Message:            "Control plane is not ready yet",
			ObservedGeneration: cluster.Generation,
		})
	}

	if cluster.Status.Ready {
		apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Ready",
			Message:            "TartCluster is ready",
			ObservedGeneration: cluster.Generation,
		})
	}

	if !cluster.Status.Ready {
		apimeta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "NotReady",
			Message:            "TartCluster is not ready yet",
			ObservedGeneration: cluster.Generation,
		})
	}

	if reflect.DeepEqual(original.Status, cluster.Status) {
		return nil
	}

	return r.Status().Patch(ctx, cluster, client.MergeFrom(original))
}

func (r *TartClusterReconciler) ensureFinalizer(ctx context.Context, cluster *infrastructurev1alpha1.TartCluster) error {
	if controllerutil.ContainsFinalizer(cluster, tartClusterFinalizer) {
		return nil
	}

	original := cluster.DeepCopy()
	controllerutil.AddFinalizer(cluster, tartClusterFinalizer)
	return r.Patch(ctx, cluster, client.MergeFrom(original))
}

func (r *TartClusterReconciler) reconcileDelete(ctx context.Context, cluster *infrastructurev1alpha1.TartCluster) error {
	if !controllerutil.ContainsFinalizer(cluster, tartClusterFinalizer) {
		return nil
	}

	original := cluster.DeepCopy()
	controllerutil.RemoveFinalizer(cluster, tartClusterFinalizer)
	return r.Patch(ctx, cluster, client.MergeFrom(original))
}

// SetupWithManager sets up the controller with the Manager.
func (r *TartClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.TartCluster{}).
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
	var tartClusterList infrastructurev1alpha1.TartClusterList
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

// isClusterPaused checks if a CAPI Cluster is paused via spec.paused or the paused annotation.
func isClusterPaused(cluster *clusterv1.Cluster) bool {
	if cluster.Spec.Paused != nil && *cluster.Spec.Paused {
		return true
	}
	if _, ok := cluster.Annotations[clusterv1.PausedAnnotation]; ok {
		return true
	}
	return false
}
