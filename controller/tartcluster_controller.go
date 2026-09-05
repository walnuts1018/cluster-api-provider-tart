package controller

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controlplane"
	clusterdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster"
)

// TartClusterReconciler reconciles a TartCluster object.
type TartClusterReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartclusters,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create

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
	if cluster.Spec.ClusterID == "" {
		original := cluster.DeepCopy()
		cluster.Spec.ClusterID = clusterdomain.NewClusterID().String()
		if err := r.Patch(ctx, &cluster, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	generation := cluster.Status.ActiveSecretGeneration
	if generation == 0 {
		generation = 1
	}
	clusterID, err := parseClusterID(cluster.Spec.ClusterID)
	if err != nil {
		return r.reportBundleError(ctx, &cluster, err)
	}
	if err := r.ensureBundle(ctx, &cluster, clusterID, generation); err != nil {
		return r.reportBundleError(ctx, &cluster, err)
	}

	original := cluster.DeepCopy()
	cluster.Status.Initialization.Provisioned = new(true)
	setCondition(&cluster.Status.Conditions, infrav1alpha1.TartClusterReadyCondition, metav1.ConditionTrue, "SecretBundleReady", "The immutable cluster secret bundle is available.", cluster.Generation)
	cluster.Status.ActiveSecretGeneration = generation
	cluster.Status.ObservedGeneration = cluster.Generation
	if err := r.Status().Patch(ctx, &cluster, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *TartClusterReconciler) ensureBundle(ctx context.Context, cluster *infrav1alpha1.TartCluster, clusterID clusterdomain.ClusterID, generation int32) error {
	name, err := controlplane.BundleName(cluster.Name, clusterID, generation)
	if err != nil {
		return err
	}

	secret := &corev1.Secret{}
	err = r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, secret)
	if apierrors.IsNotFound(err) {
		data, generateErr := controlplane.GenerateBundleData(clusterID)
		if generateErr != nil {
			return generateErr
		}
		expected, buildErr := controlplane.BuildActiveSecret(cluster.Namespace, cluster.Name, clusterID, generation, metav1.OwnerReference{
			APIVersion: infrav1alpha1.GroupVersion.String(),
			Kind:       "TartCluster",
			Name:       cluster.Name,
			UID:        cluster.UID,
		}, data)
		if buildErr != nil {
			return buildErr
		}
		if err := r.Create(ctx, expected); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return nil
			}
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	if err := controlplane.ValidateBundleSecretContract(secret, cluster.Namespace, cluster.Name, clusterID, generation, controlplane.BundleStateActive, cluster.UID); err != nil {
		return err
	}
	if err := controlplane.ValidateBundleData(secret.Data, clusterID); err != nil {
		return err
	}

	return nil
}

func (r *TartClusterReconciler) reportBundleError(ctx context.Context, cluster *infrav1alpha1.TartCluster, bundleErr error) (ctrl.Result, error) {
	original := cluster.DeepCopy()
	setCondition(&cluster.Status.Conditions, infrav1alpha1.TartClusterReadyCondition, metav1.ConditionFalse, infrav1alpha1.ReasonSecretBundleUnavailable, "The immutable cluster secret bundle is unavailable or does not satisfy its identity contract.", cluster.Generation)
	cluster.Status.ObservedGeneration = cluster.Generation
	if err := r.Status().Patch(ctx, cluster, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, bundleErr
}

func (r *TartClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1alpha1.TartCluster{}).
		Owns(&corev1.Secret{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
			clusterName := obj.GetLabels()[controlplane.ClusterNameLabel]
			if clusterName == "" {
				return nil
			}
			return []reconcile.Request{{Namespace: obj.GetNamespace(), Name: clusterName}}
		})).
		Named("tartcluster").
		Complete(r)
}
