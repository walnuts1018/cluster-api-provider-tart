package tartcontrolplane

import (
	"context"
	"errors"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos/certbuilder"
	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/controller"
	clusterdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster"
	domaincontrolplane "github.com/walnuts1018/cluster-api-provider-tart/domain/controlplane"
)

const (
	controlPlaneOrdinalLabel     = "tart.cluster.x-k8s.io/control-plane-index"
	controlPlaneEtcdMemberID     = "tart.cluster.x-k8s.io/etcd-member-id"
	controlPlaneEtcdDeleteHook   = clusterv1.PreTerminateDeleteHookAnnotationPrefix + "/tart-etcd-member"
	controlPlaneScaleDownRequeue = 30 * time.Second
	// CAPI v1.14のMachine admission webhookは未指定のNodeDeletionTimeoutSecondsを10秒にdefaultする。
	capiDefaultNodeDeletionTimeoutSeconds = int32(10)
	// reasonCATrustUpdateFailedはCA rotationの各段階でTalos machine configurationへのapplyが失敗した場合のreasonである。
	reasonCATrustUpdateFailed = "CATrustUpdateFailed"
	// reasonCAConfigurationUnrecognizedは、observationのCA trust stageが既知のいずれの段階とも一致しない場合のreasonである。
	reasonCAConfigurationUnrecognized = "CAConfigurationUnrecognized"
	// messageCAConfigurationUnrecognizedはreasonCAConfigurationUnrecognizedに対応するmessageである。
	messageCAConfigurationUnrecognized = "A control-plane Machine reports a CA trust configuration that cannot be safely classified; CA rotation is stopped."
)

const reasonMachineUnavailable = "MachineUnavailable"

var errInvalidCARotationPromotionState = errors.New("CA rotation target bundle is not pending or active")

// TartControlPlaneReconcilerはTartControlPlane objectをreconcileする。
type TartControlPlaneReconciler struct {
	client.Client

	// KubernetesUpgradeはcluster-wide Kubernetes upgradeの実行者である。nilの場合はTalos upstream実装を使う。
	KubernetesUpgrade talos.KubernetesUpgradeRunner
	// KubernetesUpgradeIdentityはupgrade leaseのholder identityである。nilの場合はhost名とprocess IDから導出する。
	KubernetesUpgradeIdentity string
	// Recorderは不正なCA rotation requestなど利用者へ通知すべき事象をKubernetes Eventとして記録する。SetupWithManagerで未設定の場合はmanagerから解決する。
	Recorder record.EventRecorder
}

// NewTartControlPlaneReconcilerはclientのみを設定したTartControlPlaneReconcilerを構築する。
// KubernetesUpgradeなどのoptional fieldはDI wiringの呼び出し元が必要に応じて後から設定する。
func NewTartControlPlaneReconciler(c client.Client) *TartControlPlaneReconciler {
	return &TartControlPlaneReconciler{Client: c}
}

type controlPlaneFailure struct {
	reason  string
	message string
}

type controlPlaneBootstrapState struct {
	initialized   bool
	etcdReady     bool
	workloadReady bool
	reason        string
	message       string
	requeueAfter  time.Duration
}

var errControlPlaneScaleDownPending = errors.New("control-plane scale-down is waiting for etcd member removal")

// scaleDownPendingErrorはscale-downが通常の待機として継続しているが、原因をrequeue reasonとして観測できるようにする。
type scaleDownPendingError struct {
	reason  string
	message string
	cause   error
}

func (e *scaleDownPendingError) Error() string {
	if e.cause != nil {
		return e.reason + ": " + e.message + ": " + e.cause.Error()
	}
	return e.reason + ": " + e.message
}

func (e *scaleDownPendingError) Unwrap() error {
	if e.cause != nil {
		return errors.Join(errControlPlaneScaleDownPending, e.cause)
	}
	return errControlPlaneScaleDownPending
}

func (f *controlPlaneFailure) Error() string {
	return f.reason + ": " + f.message
}

// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=tartcontrolplanes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=tartcontrolplanes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachinetemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigtemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigs,verbs=get;list;watch;create
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update

func (r *TartControlPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cp controlplanev1alpha1.TartControlPlane
	if err := r.Get(ctx, req.NamespacedName, &cp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if controller.IsPaused(&cp) {
		return r.reconcilePaused(ctx, &cp)
	}
	if !cp.DeletionTimestamp.IsZero() {
		return r.reconcileDeleting(ctx, &cp)
	}

	clusterName, err := controlPlaneClusterName(&cp)
	if err != nil {
		return r.reportFailure(ctx, &cp, err)
	}
	if cp.Spec.Version == "" {
		return r.reportFailure(ctx, &cp, &controlPlaneFailure{
			reason:  "InvalidSpec",
			message: "The Kubernetes version is required before control-plane Machines can be created.",
		})
	}
	desiredReplicas, err := desiredControlPlaneReplicas(&cp)
	if err != nil {
		return r.reportFailure(ctx, &cp, err)
	}

	var cluster clusterv1.Cluster
	if err := r.Get(ctx, client.ObjectKey{Namespace: cp.Namespace, Name: clusterName}, &cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return r.reportFailure(ctx, &cp, &controlPlaneFailure{
				reason:  controller.ReasonClusterUnavailable,
				message: "The referenced CAPI Cluster is not available yet.",
			})
		}
		return ctrl.Result{}, err
	}

	tartCluster, err := r.getTartCluster(ctx, &cluster)
	if err != nil {
		return r.reportFailure(ctx, &cp, err)
	}
	if err := r.validateActiveBundle(ctx, tartCluster); err != nil {
		return r.reportFailure(ctx, &cp, err)
	}

	var machineTemplate infrav1alpha1.TartMachineTemplate
	if err := r.getTartMachineTemplate(ctx, cp.Namespace, &cp.Spec.MachineTemplate.Spec.InfrastructureRef, &machineTemplate); err != nil {
		return r.reportFailure(ctx, &cp, err)
	}
	var bootstrapTemplate bootstrapv1alpha1.TartBootstrapConfigTemplate
	if err := r.getBootstrapTemplate(ctx, cp.Namespace, &cp.Spec.BootstrapConfigTemplateRef, &bootstrapTemplate); err != nil {
		return r.reportFailure(ctx, &cp, err)
	}
	machines, err := r.ensureMachines(ctx, &cp, clusterName, desiredReplicas, tartCluster.Status.FailureDomains, &machineTemplate, &bootstrapTemplate)
	scaleDownPending := errors.Is(err, errControlPlaneScaleDownPending)
	if err != nil && !scaleDownPending {
		if failure, ok := errors.AsType[*controlPlaneFailure](err); ok {
			return r.reportFailure(ctx, &cp, failure)
		}
		return ctrl.Result{}, err
	}
	bootstrapState, err := r.reconcileControlPlaneBootstrap(ctx, &cp, &cluster, machines)
	if err != nil {
		return ctrl.Result{}, err
	}
	caRotationState, err := r.reconcileCARotation(ctx, tartCluster, scaleDownPending)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Kubernetes version upgradeはcluster-wide operationであり、TartControlPlaneだけが所有する。
	// CA rotationやscale-downと同時には開始せず、他のlifecycle operationの完了を待つ。
	upgradeState := r.reconcileKubernetesUpgrade(ctx, &cp, &cluster, machines, bootstrapState, caRotationState.active || scaleDownPending, desiredReplicas)

	original := cp.DeepCopy()
	setControlPlaneStatus(&cp, clusterName, desiredReplicas, machines, bootstrapState, caRotationState, upgradeState)
	if err := r.Status().Patch(ctx, &cp, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}

	requeueAfter := bootstrapState.requeueAfter
	if caRotationState.requeueAfter > 0 && (requeueAfter == 0 || caRotationState.requeueAfter < requeueAfter) {
		requeueAfter = caRotationState.requeueAfter
	}
	if upgradeState.requeueAfter > 0 && (requeueAfter == 0 || upgradeState.requeueAfter < requeueAfter) {
		requeueAfter = upgradeState.requeueAfter
	}
	if scaleDownPending && requeueAfter == 0 {
		requeueAfter = controlPlaneScaleDownRequeue
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *TartControlPlaneReconciler) reconcilePaused(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane) (ctrl.Result, error) {
	original := cp.DeepCopy()
	controller.SetPausedCondition(&cp.Status.Conditions, true, cp.Generation)
	if err := r.Status().Patch(ctx, cp, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *TartControlPlaneReconciler) reconcileDeleting(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane) (ctrl.Result, error) {
	original := cp.DeepCopy()
	controller.SetCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneDeletingCondition, metav1.ConditionTrue, "Deleting", "The control plane is being deleted; no new Machine or Host allocation is started.", cp.Generation)
	controller.SetCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneAvailableCondition, metav1.ConditionFalse, "Deleting", "The control plane is being deleted.", cp.Generation)
	controller.SetPausedCondition(&cp.Status.Conditions, false, cp.Generation)
	cp.Status.ObservedGeneration = cp.Generation
	if err := r.Status().Patch(ctx, cp, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *TartControlPlaneReconciler) reportFailure(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane, report error) (ctrl.Result, error) {
	failure := &controlPlaneFailure{reason: "ReconcileFailed", message: "The control plane cannot proceed until its dependencies are available."}
	if failureValue, ok := errors.AsType[*controlPlaneFailure](report); ok {
		failure = failureValue
	} else if ownershipFailure, ok := errors.AsType[*controller.OwnershipFailure](report); ok {
		failure = &controlPlaneFailure{reason: ownershipFailure.Reason, message: ownershipFailure.Message}
	}
	original := cp.DeepCopy()
	controller.SetCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlaneAvailableCondition, metav1.ConditionFalse, failure.reason, failure.message, cp.Generation)
	controller.SetPausedCondition(&cp.Status.Conditions, false, cp.Generation)
	cp.Status.ObservedGeneration = cp.Generation
	if err := r.Status().Patch(ctx, cp, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func controlPlaneClusterName(cp *controlplanev1alpha1.TartControlPlane) (string, error) {
	if name := cp.Labels[clusterv1.ClusterNameLabel]; name != "" {
		return name, nil
	}
	for _, owner := range cp.OwnerReferences {
		if owner.APIVersion == clusterv1.GroupVersion.String() && owner.Kind == "Cluster" && owner.Name != "" {
			return owner.Name, nil
		}
	}
	return "", &controlPlaneFailure{
		reason:  controller.ReasonClusterUnavailable,
		message: "The TartControlPlane has no reference to a CAPI Cluster.",
	}
}

func desiredControlPlaneReplicas(cp *controlplanev1alpha1.TartControlPlane) (int32, error) {
	if cp.Spec.Replicas == nil {
		return 1, nil
	}
	if *cp.Spec.Replicas < 1 {
		return 0, &controlPlaneFailure{
			reason:  "InvalidSpec",
			message: "The control-plane replica count must be at least one to preserve an etcd member.",
		}
	}
	return *cp.Spec.Replicas, nil
}

func (r *TartControlPlaneReconciler) getTartCluster(ctx context.Context, cluster *clusterv1.Cluster) (*infrav1alpha1.TartCluster, error) {
	ref := cluster.Spec.InfrastructureRef
	if ref.APIGroup != infrav1alpha1.GroupVersion.Group || ref.Kind != controller.TartClusterKind || ref.Name == "" {
		return nil, &controlPlaneFailure{
			reason:  controller.ReasonClusterUnavailable,
			message: "The CAPI Cluster does not reference a TartCluster infrastructure resource.",
		}
	}
	var tartCluster infrav1alpha1.TartCluster
	if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: ref.Name}, &tartCluster); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, &controlPlaneFailure{
				reason:  controller.ReasonClusterUnavailable,
				message: "The referenced TartCluster is not available yet.",
			}
		}
		return nil, err
	}
	if tartCluster.Spec.ClusterID == "" || tartCluster.Status.ActiveSecretGeneration < 1 {
		return nil, &controlPlaneFailure{
			reason:  controller.ReasonSecretBundleUnavailable,
			message: "The TartCluster identity and active secret bundle are not ready yet.",
		}
	}
	if _, err := clusterdomain.ParseClusterID(tartCluster.Spec.ClusterID); err != nil {
		return nil, &controlPlaneFailure{
			reason:  controller.ReasonSecretBundleUnavailable,
			message: "The TartCluster identity is invalid.",
		}
	}
	return &tartCluster, nil
}

func (r *TartControlPlaneReconciler) validateActiveBundle(ctx context.Context, cluster *infrav1alpha1.TartCluster) error {
	generation := cluster.Status.ActiveSecretGeneration
	clusterID, err := clusterdomain.ParseClusterID(cluster.Spec.ClusterID)
	if err != nil {
		return &controlPlaneFailure{
			reason:  controller.ReasonSecretBundleUnavailable,
			message: "The TartCluster identity is invalid.",
		}
	}
	name, err := domaincontrolplane.BundleName(cluster.Name, clusterID, generation)
	if err != nil {
		return &controlPlaneFailure{
			reason:  controller.ReasonSecretBundleUnavailable,
			message: "The active cluster secret bundle identity is invalid.",
		}
	}
	var secret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return &controlPlaneFailure{
				reason:  controller.ReasonSecretBundleUnavailable,
				message: "The active cluster secret bundle is not available yet.",
			}
		}
		return err
	}
	if err := domaincontrolplane.ValidateBundleSecretContract(&secret, cluster.Namespace, cluster.Name, clusterID, generation, domaincontrolplane.BundleStateActive, cluster.UID); err != nil {
		return &controlPlaneFailure{
			reason:  controller.ReasonSecretBundleUnavailable,
			message: "The active cluster secret bundle does not satisfy its identity contract.",
		}
	}
	if err := certbuilder.ValidateBundleData(secret.Data, clusterID); err != nil {
		return &controlPlaneFailure{
			reason:  controller.ReasonSecretBundleUnavailable,
			message: "The active cluster secret bundle data is invalid.",
		}
	}
	return nil
}

func (r *TartControlPlaneReconciler) enqueueAllControlPlanes(ctx context.Context, _ client.Object) []reconcile.Request {
	var list controlplanev1alpha1.TartControlPlaneList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
	}
	return requests
}

func (r *TartControlPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("tartcontrolplane-controller") //nolint:staticcheck // record.EventRecorderを型として使う既存箇所と合わせるため、新APIへの移行は別途まとめて行う
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&controlplanev1alpha1.TartControlPlane{}).
		Owns(&clusterv1.Machine{}).
		Watches(&clusterv1.Cluster{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllControlPlanes)).
		Watches(&infrav1alpha1.TartMachine{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllControlPlanes)).
		Watches(&bootstrapv1alpha1.TartBootstrapConfig{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllControlPlanes)).
		Watches(&infrav1alpha1.TartCluster{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllControlPlanes)).
		Watches(&infrav1alpha1.TartMachineTemplate{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllControlPlanes)).
		Watches(&bootstrapv1alpha1.TartBootstrapConfigTemplate{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllControlPlanes)).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.enqueueAllControlPlanes)).
		Named("tartcontrolplane").
		Complete(r)
}
