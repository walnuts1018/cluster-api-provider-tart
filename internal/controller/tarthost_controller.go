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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
)

// TartHostReconciler reconciles a TartHost object
type TartHostReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	HostService hostdomain.Service
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
		if err := r.hostService().MarkAvailable(ctx, &host, "InventoryReady", "Host is available for TartMachine assignment"); err != nil {
			return ctrl.Result{}, err
		}
		log.Info("Set TartHost to available state", "host", req.String())
	}

	if host.Status.MachineRef != nil {
		released, err := r.hostService().ReleaseMissingReference(ctx, &host)
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

func (r *TartHostReconciler) hostService() hostdomain.Service {
	if r.HostService != nil {
		return r.HostService
	}
	return hostdomain.NewService(r.Client)
}

func (r *TartHostReconciler) tartMachineToReferencedTartHosts(ctx context.Context, obj client.Object) []reconcile.Request {
	machine, ok := obj.(*infrastructurev1alpha1.TartMachine)
	if !ok {
		return nil
	}

	var hosts infrastructurev1alpha1.TartHostList
	if err := r.List(
		ctx,
		&hosts,
		client.InNamespace(machine.Namespace),
		client.MatchingFields{tartHostMachineRefField: tartHostMachineRefIndexValueForMachine(machine)},
	); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, 0)
	for i := range hosts.Items {
		host := &hosts.Items[i]
		if machineRefMatches(host.Status.MachineRef, machine) {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(host),
			})
		}
	}

	return requests
}
