package tartcluster

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

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos/certbuilder"
	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controller"
	clusterdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster"
	domaincontrolplane "github.com/walnuts1018/cluster-api-provider-tart/domain/controlplane"
	hostusecase "github.com/walnuts1018/cluster-api-provider-tart/usecase/host"
	"k8s.io/apimachinery/pkg/api/meta"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// TartClusterReconcilerはTartCluster objectをreconcileする。
type TartClusterReconciler struct {
	client.Client
}

// NewTartClusterReconcilerはclientのみを設定したTartClusterReconcilerを構築する。
func NewTartClusterReconciler(c client.Client) *TartClusterReconciler {
	return &TartClusterReconciler{Client: c}
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartclusters,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=list;watch
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=tartcontrolplanes,verbs=list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create

func (r *TartClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cluster infrav1alpha1.TartCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if controller.IsPaused(&cluster) {
		return ctrl.Result{}, nil
	}
	if !cluster.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// TartCluster.spec.idは具体的な(non-dry-run)Resource作成後に一度だけ生成し、再生成しない。設定前にsecret bundle生成、Host claim、provisioningを開始しない。
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
	clusterID, err := clusterdomain.ParseClusterID(cluster.Spec.ClusterID)
	if err != nil {
		return r.reportBundleError(ctx, &cluster, err)
	}
	if err := r.ensureBundle(ctx, &cluster, clusterID, generation); err != nil {
		return r.reportBundleError(ctx, &cluster, err)
	}
	if err := r.ensureCARotationBundle(ctx, &cluster, clusterID, generation); err != nil {
		return r.reportBundleError(ctx, &cluster, err)
	}
	failureDomains, err := r.observeFailureDomains(ctx)
	if err != nil {
		return r.reportFailureDomainError(ctx, &cluster, err)
	}

	// status.initialization.provisionedはsecret bundleが利用可能になった時点でtrueにする。
	// TartControlPlaneの子Machine作成はこのProvisioned(CAPIのInfrastructureReady)を前提に
	// 開始するため、ここでTartControlPlaneのreadinessを待つと循環依存になり両者とも
	// 進行できなくなる。Control Plane/Infrastructureの健全性はReady Conditionの内容にのみ
	// 反映し、Provisionedのgate自体は変更しない。
	readyStatus, readyReason, readyMessage, err := r.aggregateReadiness(ctx, &cluster)
	if err != nil {
		return r.reportFailureDomainError(ctx, &cluster, err)
	}

	original := cluster.DeepCopy()
	cluster.Status.Initialization.Provisioned = new(true)
	cluster.Status.FailureDomains = failureDomains
	controller.SetCondition(&cluster.Status.Conditions, infrav1alpha1.TartClusterReadyCondition, readyStatus, readyReason, readyMessage, cluster.Generation)
	cluster.Status.ActiveSecretGeneration = generation
	cluster.Status.ObservedGeneration = cluster.Generation
	if err := r.Status().Patch(ctx, &cluster, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// aggregateReadinessは、secret bundleに加えてこのClusterに紐づくTartControlPlaneのAvailable
// Conditionを観測してReady Conditionの内容を決定する。TartControlPlaneがまだ存在しない場合
// (control planeの初回provisioning前)は、secret bundleの準備完了だけをもってReady=Trueとする。
func (r *TartClusterReconciler) aggregateReadiness(ctx context.Context, cluster *infrav1alpha1.TartCluster) (metav1.ConditionStatus, string, string, error) {
	if cluster.Name == "" {
		return metav1.ConditionTrue, "SecretBundleReady", "The immutable cluster secret bundle is available.", nil
	}

	var controlPlanes controlplanev1alpha1.TartControlPlaneList
	if err := r.List(ctx, &controlPlanes, client.InNamespace(cluster.Namespace), client.MatchingLabels{clusterv1.ClusterNameLabel: cluster.Name}); err != nil {
		return "", "", "", err
	}
	if len(controlPlanes.Items) == 0 {
		return metav1.ConditionTrue, "SecretBundleReady", "The immutable cluster secret bundle is available.", nil
	}

	controlPlane := &controlPlanes.Items[0]
	available := meta.FindStatusCondition(controlPlane.Status.Conditions, controlplanev1alpha1.TartControlPlaneAvailableCondition)
	if available == nil || available.Status != metav1.ConditionTrue {
		return metav1.ConditionFalse, "ControlPlaneNotAvailable", "The TartControlPlane for this Cluster is not yet Available.", nil
	}
	return metav1.ConditionTrue, "ControlPlaneAvailable", "The immutable cluster secret bundle is available and the TartControlPlane is Available.", nil
}

func (r *TartClusterReconciler) observeFailureDomains(ctx context.Context) ([]clusterv1.FailureDomain, error) {
	hosts := &infrav1alpha1.TartHostList{}
	if err := r.List(ctx, hosts); err != nil {
		return nil, err
	}
	return hostusecase.FailureDomains(hosts.Items), nil
}

func (r *TartClusterReconciler) ensureBundle(ctx context.Context, cluster *infrav1alpha1.TartCluster, clusterID clusterdomain.ClusterID, generation int32) error {
	name, err := domaincontrolplane.BundleName(cluster.Name, clusterID, generation)
	if err != nil {
		return err
	}

	secret := &corev1.Secret{}
	err = r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, secret)
	if apierrors.IsNotFound(err) {
		data, generateErr := certbuilder.GenerateBundleData(clusterID)
		if generateErr != nil {
			return generateErr
		}
		expected, buildErr := domaincontrolplane.BuildActiveSecret(cluster.Namespace, cluster.Name, clusterID, generation, metav1.OwnerReference{
			APIVersion: infrav1alpha1.GroupVersion.String(),
			Kind:       controller.TartClusterKind,
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
	if err := domaincontrolplane.ValidateBundleSecretContract(secret, cluster.Namespace, cluster.Name, clusterID, generation, domaincontrolplane.BundleStateActive, cluster.UID); err != nil {
		return err
	}
	if err := certbuilder.ValidateBundleData(secret.Data, clusterID); err != nil {
		return err
	}

	return nil
}

// ensureCARotationBundleはTartCluster.spec.caRotationRequestedGenerationがactive generationの次世代を指している場合に、そのgenerationのPending bundle Secretを先行生成する。
// Talos公式の段階的CA更新は、この不変なPending bundleとactive bundleを比較してTartControlPlaneがreconcileする。ここではSecretの先行生成だけを担い、実際のTalos側切替やactive generationの昇格はTartControlPlaneの責務とする。
func (r *TartClusterReconciler) ensureCARotationBundle(ctx context.Context, cluster *infrav1alpha1.TartCluster, clusterID clusterdomain.ClusterID, activeGeneration int32) error {
	requested := cluster.Spec.CARotationRequestedGeneration
	if requested == nil {
		return nil
	}
	target, err := domaincontrolplane.NextGeneration(activeGeneration)
	if err != nil {
		return err
	}
	if *requested != target {
		// 要求されたgenerationが次世代と一致しない場合、Pending bundleの先行生成はここで何もしない。
		// 既に昇格済み、または無効な要求のいずれかであり、区別できないためこの関数ではsilentに
		// no-opとするが、利用者への通知はTartControlPlane側のreconcileCARotationが同じ判定を
		// 行い、"InvalidCARotationRequest" reason/Conditionとwarning Eventとして表面化する。
		return nil
	}

	name, err := domaincontrolplane.BundleName(cluster.Name, clusterID, target)
	if err != nil {
		return err
	}
	secret := &corev1.Secret{}
	err = r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, secret)
	if apierrors.IsNotFound(err) {
		activeName, activeErr := domaincontrolplane.BundleName(cluster.Name, clusterID, activeGeneration)
		if activeErr != nil {
			return activeErr
		}
		activeSecret := &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: activeName}, activeSecret); err != nil {
			return err
		}
		if err := domaincontrolplane.ValidateBundleSecretContract(activeSecret, cluster.Namespace, cluster.Name, clusterID, activeGeneration, domaincontrolplane.BundleStateActive, cluster.UID); err != nil {
			return err
		}
		activeBundle, err := certbuilder.DecodeBundleData(activeSecret.Data, clusterID)
		if err != nil {
			return err
		}
		data, err := certbuilder.GenerateRotatedBundleData(clusterID, activeBundle)
		if err != nil {
			return err
		}
		expected, err := domaincontrolplane.BuildPendingSecret(cluster.Namespace, cluster.Name, clusterID, target, metav1.OwnerReference{
			APIVersion: infrav1alpha1.GroupVersion.String(),
			Kind:       controller.TartClusterKind,
			Name:       cluster.Name,
			UID:        cluster.UID,
		}, data)
		if err != nil {
			return err
		}
		if err := r.Create(ctx, expected); err != nil && !apierrors.IsAlreadyExists(err) {
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	if err := domaincontrolplane.ValidateBundleSecretContract(secret, cluster.Namespace, cluster.Name, clusterID, target, domaincontrolplane.BundleStatePending, cluster.UID); err != nil {
		return err
	}
	return certbuilder.ValidateBundleData(secret.Data, clusterID)
}

func (r *TartClusterReconciler) reportBundleError(ctx context.Context, cluster *infrav1alpha1.TartCluster, bundleErr error) (ctrl.Result, error) {
	original := cluster.DeepCopy()
	controller.SetCondition(&cluster.Status.Conditions, infrav1alpha1.TartClusterReadyCondition, metav1.ConditionFalse, infrav1alpha1.ReasonSecretBundleUnavailable, "The immutable cluster secret bundle is unavailable or does not satisfy its identity contract.", cluster.Generation)
	cluster.Status.ObservedGeneration = cluster.Generation
	if err := r.Status().Patch(ctx, cluster, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, bundleErr
}

func (r *TartClusterReconciler) reportFailureDomainError(ctx context.Context, cluster *infrav1alpha1.TartCluster, observeErr error) (ctrl.Result, error) {
	original := cluster.DeepCopy()
	cluster.Status.FailureDomains = nil
	controller.SetCondition(&cluster.Status.Conditions, infrav1alpha1.TartClusterReadyCondition, metav1.ConditionFalse, "FailureDomainsUnavailable", "The complete Host inventory could not be observed; failure domains are not surfaced.", cluster.Generation)
	cluster.Status.ObservedGeneration = cluster.Generation
	if err := r.Status().Patch(ctx, cluster, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, observeErr
}

func (r *TartClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1alpha1.TartCluster{}).
		Owns(&corev1.Secret{}).
		Watches(&infrav1alpha1.TartHost{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllClusters)).
		Watches(&controlplanev1alpha1.TartControlPlane{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
			clusterName := obj.GetLabels()[clusterv1.ClusterNameLabel]
			if clusterName == "" {
				return nil
			}
			return []reconcile.Request{{NamespacedName: client.ObjectKey{Namespace: obj.GetNamespace(), Name: clusterName}}} //nolint:modernize // NamespacedNameのfield名を明示した方がこの箇所では読みやすい
		})).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
			clusterName := obj.GetLabels()[domaincontrolplane.ClusterNameLabel]
			if clusterName == "" {
				return nil
			}
			return []reconcile.Request{{Namespace: obj.GetNamespace(), Name: clusterName}}
		})).
		Named("tartcluster").
		Complete(r)
}

func (r *TartClusterReconciler) enqueueAllClusters(ctx context.Context, _ client.Object) []reconcile.Request {
	clusters := &infrav1alpha1.TartClusterList{}
	if err := r.List(ctx, clusters); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0, len(clusters.Items))
	for index := range clusters.Items {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&clusters.Items[index])})
	}
	return requests
}
