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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	controlplanev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/controlplane/v1alpha1"
)

// TartControlPlaneReconciler reconciles a TartControlPlane object.
type TartControlPlaneReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=tartcontrolplanes,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=tartcontrolplanes/status,verbs=get;update;patch

func (r *TartControlPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cp controlplanev1alpha1.TartControlPlane
	if err := r.Get(ctx, req.NamespacedName, &cp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if isPaused(&cp) {
		return r.reconcilePaused(ctx, &cp)
	}

	// TODO: cluster secret bundle生成、初回etcd bootstrap、quorum-safeなscale up/down、
	// kubeconfig Secret管理をcontrolplaneパッケージの実装後にここへ接続する。
	original := cp.DeepCopy()
	meta.SetStatusCondition(&cp.Status.Conditions, metav1.Condition{
		Type:               controlplanev1alpha1.TartControlPlaneReadyCondition,
		Status:             metav1.ConditionFalse,
		Reason:             "NotImplemented",
		Message:            "Reconcile logic for this resource is not implemented yet; no external side effect has been attempted.",
		ObservedGeneration: cp.Generation,
	})
	setPausedCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlanePausedCondition, false, cp.Generation)
	cp.Status.ObservedGeneration = cp.Generation
	if err := r.Status().Patch(ctx, &cp, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *TartControlPlaneReconciler) reconcilePaused(ctx context.Context, cp *controlplanev1alpha1.TartControlPlane) (ctrl.Result, error) {
	original := cp.DeepCopy()
	setPausedCondition(&cp.Status.Conditions, controlplanev1alpha1.TartControlPlanePausedCondition, true, cp.Generation)
	if err := r.Status().Patch(ctx, cp, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *TartControlPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&controlplanev1alpha1.TartControlPlane{}).
		Named("tartcontrolplane").
		Complete(r)
}
