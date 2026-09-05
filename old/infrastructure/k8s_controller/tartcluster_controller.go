package controller

import (
	"context"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/utils/telemetry"
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

	clusterlifecycle "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster/workflow/reconcile_cluster"
	clusterstatus "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster/workflow/reconcile_cluster_status"
	resourcefinalizer "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/resource_finalizer"
	"github.com/walnuts1018/cluster-api-provider-tart/infrastructure/workflowresult"
)

// TartClusterReconciler reconciles a TartCluster object
type TartClusterReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Lifecycle *clusterlifecycle.Workflow
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
	event, err := workflowresult.Unwrap(r.lifecycleWorkflow().Do(ctx, clusterlifecycle.Command{Cluster: &cluster}))
	if err != nil {
		return ctrl.Result{}, err
	}
	logClusterLifecycle(ctx, &cluster, event.(clusterlifecycle.ClusterReconciled).Result)

	return ctrl.Result{}, nil
}

func logClusterLifecycle(
	ctx context.Context,
	cluster *infrastructurev1beta1.TartCluster,
	result clusterlifecycle.Result,
) {
	reconciled, ok := result.(clusterlifecycle.ResultActiveReconciled)
	if !ok {
		return
	}

	log := logf.FromContext(ctx)
	switch observed := reconciled.Status.(type) {
	case clusterstatus.ResultSkippedMissingClusterLabel:
		log.V(4).Info("TartCluster missing cluster label, skipping", "cluster", cluster.Name)
	case clusterstatus.ResultSkippedClusterNotFound:
		log.V(4).Info("Cluster not found, skipping reconciliation", "cluster", observed.ClusterName)
	case clusterstatus.ResultSkippedPausedCluster:
		log.V(4).Info("Cluster is paused, skipping reconciliation", "cluster", observed.ClusterName)
	}
}

func (r *TartClusterReconciler) lifecycleWorkflow() *clusterlifecycle.Workflow {
	if r.Lifecycle != nil {
		return r.Lifecycle
	}
	return clusterlifecycle.NewWorkflowWithPorts(
		resourcefinalizer.NewTartClusterService(r.Client),
		clusterstatus.NewWorkflow(r.Client),
	)
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
