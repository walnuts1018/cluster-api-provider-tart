// Package controller contains the thin Kubernetes watch/reconcile entrypoints for Tart resources.
package controller

import (
	"context"
	"time"

	"uuid"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/host"
)

const tartHostFinalizer = "tart.cluster.x-k8s.io/host-lifecycle"

// TartHostReconcilerはHost identityと削除時のretention gateを管理する。Talos discoveryや
// power操作は観測adapterが接続されるまで開始せず、Statusだけを安全側へ更新する。
type TartHostReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts/status,verbs=get;update;patch

func (r *TartHostReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var current infrav1alpha1.TartHost
	if err := r.Get(ctx, req.NamespacedName, &current); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if isPaused(&current) {
		return ctrl.Result{}, nil
	}

	if !current.DeletionTimestamp.IsZero() {
		return r.reconcileDeletion(ctx, &current)
	}
	if current.Spec.ID == "" {
		original := current.DeepCopy()
		current.Spec.ID = uuid.NewV4().String()
		if err := r.Patch(ctx, &current, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	if !controllerutil.ContainsFinalizer(&current, tartHostFinalizer) {
		original := current.DeepCopy()
		controllerutil.AddFinalizer(&current, tartHostFinalizer)
		if err := r.Patch(ctx, &current, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	hosts := &infrav1alpha1.TartHostList{}
	if err := r.List(ctx, hosts); err != nil {
		return ctrl.Result{}, err
	}
	if host.HasIdentityConflictForAny(hosts.Items) {
		return r.reportIdentityConflicts(ctx, hosts.Items)
	}

	eligibility := host.Classify(current.Spec)
	original := current.DeepCopy()
	setEligibilityConditions(&current, eligibility)
	setCondition(&current.Status.Conditions, infrav1alpha1.TartHostReadyCondition, metav1.ConditionFalse, infrav1alpha1.ReasonNotImplemented, "Host discovery and Talos reachability observation are not implemented yet; no external side effect has been attempted.", current.Generation)
	current.Status.ObservedGeneration = current.Generation
	if err := r.Status().Patch(ctx, &current, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func setEligibilityConditions(hostObject *infrav1alpha1.TartHost, eligibility host.Eligibility) {
	generation := hostObject.Generation
	switch eligibility {
	case host.Available:
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostClaimedCondition, metav1.ConditionFalse, "NotClaimed", "The Host has no active consumerRef.", generation)
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostRetainedCondition, metav1.ConditionFalse, "NotRetained", "The Host is not retained from a previous Machine.", generation)
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostReusableCondition, metav1.ConditionFalse, "NotReusable", "The Host has no retained state to reuse.", generation)
	case host.Claimed:
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostClaimedCondition, metav1.ConditionTrue, "Claimed", "The Host is claimed by a TartMachine.", generation)
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostRetainedCondition, metav1.ConditionFalse, "NotRetained", "The Host is not retained from a previous Machine.", generation)
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostReusableCondition, metav1.ConditionFalse, "NotReusable", "The Host is currently claimed.", generation)
	case host.Retained:
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostClaimedCondition, metav1.ConditionFalse, "NotClaimed", "The Host has no active consumerRef.", generation)
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostRetainedCondition, metav1.ConditionTrue, "Retained", "The Host retains state from a previous TartMachine.", generation)
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostReusableCondition, metav1.ConditionFalse, "ReuseApprovalRequired", "A matching reuse approval and reuse mode are required.", generation)
	case host.Reusable:
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostClaimedCondition, metav1.ConditionFalse, "NotClaimed", "The Host has no active consumerRef.", generation)
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostRetainedCondition, metav1.ConditionTrue, "Retained", "The Host retains state from a previous TartMachine.", generation)
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostReusableCondition, metav1.ConditionTrue, "ReuseApproved", "The Host has an explicit matching reuse approval.", generation)
	default:
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostClaimedCondition, metav1.ConditionFalse, "NotClaimed", "The Host has no active consumerRef.", generation)
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostRetainedCondition, metav1.ConditionFalse, "NotRetained", "The Host is not retained from a previous Machine.", generation)
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostReusableCondition, metav1.ConditionFalse, "NotReusable", "The Host has no retained state to reuse.", generation)
	}

	if hostObject.Status.Inventory != nil {
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostInventoryReadyCondition, metav1.ConditionTrue, "InventoryObserved", "Hardware inventory has been observed.", generation)
	} else {
		setCondition(&hostObject.Status.Conditions, infrav1alpha1.TartHostInventoryReadyCondition, metav1.ConditionFalse, "InventoryUnavailable", "Hardware inventory has not been observed yet.", generation)
	}
}

func (r *TartHostReconciler) reconcileDeletion(ctx context.Context, current *infrav1alpha1.TartHost) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(current, tartHostFinalizer) {
		return ctrl.Result{}, nil
	}
	if !forgetApproved(current.Spec) {
		return r.report(ctx, current, infrav1alpha1.ReasonForgetApprovalRequired, "The Host is claimed or retained; matching forget approval is required and no power or data operation is performed.")
	}
	original := current.DeepCopy()
	controllerutil.RemoveFinalizer(current, tartHostFinalizer)
	if err := r.Patch(ctx, current, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func forgetApproved(spec infrav1alpha1.TartHostSpec) bool {
	if spec.ConsumerRef == nil && spec.RetainedFrom == nil {
		return true
	}
	if spec.ForgetApproval == nil {
		return false
	}
	if spec.ConsumerRef != nil {
		if spec.ConsumerRef.UID == "" || spec.ForgetApproval.ConsumerUID == "" || spec.ForgetApproval.ConsumerUID != spec.ConsumerRef.UID {
			return false
		}
	}
	if spec.RetainedFrom != nil {
		if spec.RetainedFrom.UID == "" || spec.ForgetApproval.RetainedFromUID == "" || spec.ForgetApproval.RetainedFromUID != spec.RetainedFrom.UID {
			return false
		}
	}
	return true
}

func (r *TartHostReconciler) report(ctx context.Context, current *infrav1alpha1.TartHost, reason, message string) (ctrl.Result, error) {
	original := current.DeepCopy()
	setCondition(&current.Status.Conditions, infrav1alpha1.TartHostReadyCondition, metav1.ConditionFalse, reason, message, current.Generation)
	current.Status.ObservedGeneration = current.Generation
	if err := r.Status().Patch(ctx, current, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *TartHostReconciler) reportIdentityConflicts(ctx context.Context, hosts []infrav1alpha1.TartHost) (ctrl.Result, error) {
	for index := range hosts {
		candidate := &hosts[index]
		if isPaused(candidate) || !host.HasIdentityConflict(*candidate, hosts) {
			continue
		}

		original := candidate.DeepCopy()
		setCondition(&candidate.Status.Conditions, infrav1alpha1.TartHostReadyCondition, metav1.ConditionFalse, infrav1alpha1.ReasonIdentityConflict, "Stable Host identity is duplicated; allocation and maintenance configuration are stopped.", candidate.Generation)
		candidate.Status.ObservedGeneration = candidate.Generation
		if err := r.Status().Patch(ctx, candidate, client.MergeFrom(original)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *TartHostReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1alpha1.TartHost{}).
		Named("tarthost").
		Complete(r)
}
