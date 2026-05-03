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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
)

// TartHostReconciler reconciles a TartHost object
type TartHostReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts/finalizers,verbs=update
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the TartHost object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.3/pkg/reconcile
func (r *TartHostReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var host infrastructurev1alpha1.TartHost
	if err := r.Get(ctx, req.NamespacedName, &host); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if host.Status.State == "" {
		if err := r.markHostAvailable(ctx, &host, "InventoryReady", "Host is available for TartMachine assignment"); err != nil {
			return ctrl.Result{}, err
		}
		log.Info("TartHost を利用可能状態にしました", "host", req.String())
	}

	if host.Status.MachineRef != nil {
		released, err := r.releaseMissingMachineReference(ctx, &host)
		if err != nil {
			return ctrl.Result{}, err
		}
		if released {
			log.Info("存在しない TartMachine への参照を解放しました", "host", req.String())
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TartHostReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.TartHost{}).
		Watches(
			&infrastructurev1alpha1.TartMachine{},
			handler.EnqueueRequestsFromMapFunc(r.tartMachineToReferencedTartHosts),
		).
		Named("tarthost").
		Complete(r)
}

func (r *TartHostReconciler) releaseMissingMachineReference(ctx context.Context, host *infrastructurev1alpha1.TartHost) (bool, error) {
	ref := host.Status.MachineRef
	if ref == nil {
		return false, nil
	}

	var machine infrastructurev1alpha1.TartMachine
	err := r.Get(ctx, client.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}, &machine)
	if err == nil && machine.UID == ref.UID {
		return false, nil
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return false, err
	}
	if err == nil && ref.UID != "" && machine.UID != ref.UID {
		return false, r.markHostAvailable(ctx, host, "StaleMachineReference", fmt.Sprintf("Host reference to TartMachine %s/%s became stale", ref.Namespace, ref.Name))
	}

	return true, r.markHostAvailable(ctx, host, "MachineMissing", fmt.Sprintf("Released stale TartMachine reference %s/%s", ref.Namespace, ref.Name))
}

func (r *TartHostReconciler) markHostAvailable(ctx context.Context, host *infrastructurev1alpha1.TartHost, reason, message string) error {
	original := host.DeepCopy()
	host.Status.State = infrastructurev1alpha1.TartHostStateAvailable
	host.Status.MachineRef = nil
	host.Status.ObservedGeneration = host.Generation
	apimeta.SetStatusCondition(&host.Status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: host.Generation,
	})
	return r.Status().Patch(ctx, host, client.MergeFrom(original))
}

func (r *TartHostReconciler) tartMachineToReferencedTartHosts(ctx context.Context, obj client.Object) []reconcile.Request {
	machine, ok := obj.(*infrastructurev1alpha1.TartMachine)
	if !ok {
		return nil
	}

	var hosts infrastructurev1alpha1.TartHostList
	if err := r.List(ctx, &hosts, client.InNamespace(machine.Namespace)); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, 0)
	for i := range hosts.Items {
		host := &hosts.Items[i]
		if referencesTartMachine(host.Status.MachineRef, machine) {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(host),
			})
		}
	}

	return requests
}

func referencesTartMachine(ref *corev1.ObjectReference, machine *infrastructurev1alpha1.TartMachine) bool {
	if ref == nil {
		return false
	}
	if ref.Name != machine.Name || ref.Namespace != machine.Namespace {
		return false
	}
	return ref.UID == "" || machine.UID == "" || ref.UID == machine.UID
}
