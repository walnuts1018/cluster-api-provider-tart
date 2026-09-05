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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/bootstrap"
)

// TartBootstrapConfigReconciler reconciles a TartBootstrapConfig object.
type TartBootstrapConfigReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *TartBootstrapConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var config bootstrapv1alpha1.TartBootstrapConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if isPaused(&config) {
		return ctrl.Result{}, nil
	}

	if config.Spec.ConfigSecretRef != nil {
		input := &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: config.Namespace, Name: config.Spec.ConfigSecretRef.Name}, input); err != nil {
			if apierrors.IsNotFound(err) {
				return r.report(ctx, &config, "ConfigurationSecretUnavailable", "The referenced immutable configuration Secret is not available.")
			}
			return ctrl.Result{}, err
		}
		if err := bootstrap.ValidateConfigSecret(input); err != nil {
			return r.report(ctx, &config, "ConfigurationConflict", "The referenced configuration Secret must be immutable and contain configuration data.")
		}
	}

	// complete Talos configurationのrender contextが揃うまでraw patchをBootstrap Secretへ
	// 誤って配布しない。生成後のSecretは書き換えず、mutableな変更はUpdate Extensionへ委譲する。
	original := config.DeepCopy()
	setCondition(&config.Status.Conditions, bootstrapv1alpha1.TartBootstrapConfigReadyCondition, metav1.ConditionFalse, "NotImplemented", "Bootstrap configuration rendering is not implemented yet; no Secret has been generated.", config.Generation)
	config.Status.ObservedGeneration = config.Generation
	if err := r.Status().Patch(ctx, &config, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *TartBootstrapConfigReconciler) report(ctx context.Context, config *bootstrapv1alpha1.TartBootstrapConfig, reason, message string) (ctrl.Result, error) {
	original := config.DeepCopy()
	setCondition(&config.Status.Conditions, bootstrapv1alpha1.TartBootstrapConfigReadyCondition, metav1.ConditionFalse, reason, message, config.Generation)
	config.Status.ObservedGeneration = config.Generation
	if err := r.Status().Patch(ctx, config, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *TartBootstrapConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &bootstrapv1alpha1.TartBootstrapConfig{}, bootstrapConfigSecretIndex, func(obj client.Object) []string {
		config, ok := obj.(*bootstrapv1alpha1.TartBootstrapConfig)
		if !ok || config.Spec.ConfigSecretRef == nil {
			return nil
		}
		return []string{config.Spec.ConfigSecretRef.Name}
	}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&bootstrapv1alpha1.TartBootstrapConfig{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			configs := &bootstrapv1alpha1.TartBootstrapConfigList{}
			if err := r.List(ctx, configs, client.InNamespace(obj.GetNamespace()), client.MatchingFields{bootstrapConfigSecretIndex: obj.GetName()}); err != nil {
				return nil
			}
			requests := make([]reconcile.Request, 0, len(configs.Items))
			for index := range configs.Items {
				requests = append(requests, reconcile.Request{Namespace: configs.Items[index].Namespace, Name: configs.Items[index].Name})
			}
			return requests
		})).
		Named("tartbootstrapconfig").
		Complete(r)
}

const bootstrapConfigSecretIndex = ".spec.configSecretRef.name"
