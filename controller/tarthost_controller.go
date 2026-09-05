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

// Package controller contains the thin Kubernetes watch/reconcile entrypoints for
// each Tart Resource. Reconcilers read Kubernetes desired state, call into the host,
// talos, bootstrap, controlplane and boot packages for policy decisions and external
// side effects, and patch back only observed state and Conditions. See
// .agents/skills/reconcile/SKILL.md.
package controller

import (
	"context"

	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

// TartHostReconciler reconciles a TartHost object.
type TartHostReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts/status,verbs=get;update;patch

func (r *TartHostReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var host infrav1alpha1.TartHost
	if err := r.Get(ctx, req.NamespacedName, &host); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// TartHost.spec.id must be generated exactly once, after the concrete (non-dry-run)
	// Resource is created, and must never be regenerated. Allocation, maintenance
	// configuration apply and inventory observation must not start before it is set.
	if host.Spec.ID == "" {
		original := host.DeepCopy()
		host.Spec.ID = uuid.NewString()
		if err := r.Patch(ctx, &host, client.MergeFrom(original)); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// TODO: Host claim CAS、hardware discovery、power/boot観測、Retained/Reusable判定を
	// 次セッションで実装する(host, boot, talosパッケージを参照)。それまでは安全側で
	// Ready=Falseのまま停止し、外部副作用を一切開始しない。
	original := host.DeepCopy()
	setNotImplemented(&host.Status.Conditions, infrav1alpha1.TartHostReadyCondition, host.Generation)
	host.Status.ObservedGeneration = host.Generation
	if err := r.Status().Patch(ctx, &host, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *TartHostReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1alpha1.TartHost{}).
		Named("tarthost").
		Complete(r)
}
