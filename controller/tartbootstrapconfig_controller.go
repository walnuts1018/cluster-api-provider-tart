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

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
)

// TartBootstrapConfigReconciler reconciles a TartBootstrapConfig object.
type TartBootstrapConfigReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigs/status,verbs=get;update;patch

func (r *TartBootstrapConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var config bootstrapv1alpha1.TartBootstrapConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// TODO: bootstrapパッケージのconfiguration合成とBootstrap Secret生成を次セッションで
	// 実装する。生成後のSecretは書き換えず、mutableな変更はUpdate Extensionへ委譲する。
	original := config.DeepCopy()
	meta.SetStatusCondition(&config.Status.Conditions, metav1.Condition{
		Type:               bootstrapv1alpha1.TartBootstrapConfigReadyCondition,
		Status:             metav1.ConditionFalse,
		Reason:             "NotImplemented",
		Message:            "Reconcile logic for this resource is not implemented yet; no external side effect has been attempted.",
		ObservedGeneration: config.Generation,
	})
	config.Status.ObservedGeneration = config.Generation
	if err := r.Status().Patch(ctx, &config, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *TartBootstrapConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bootstrapv1alpha1.TartBootstrapConfig{}).
		Named("tartbootstrapconfig").
		Complete(r)
}
