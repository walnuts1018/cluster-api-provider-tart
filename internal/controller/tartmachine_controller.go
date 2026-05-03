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
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/uuid"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
)

// TartMachineReconciler reconciles a TartMachine object
type TartMachineReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const bootstrapTokenTTL = 10 * time.Minute

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines/finalizers,verbs=update
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts/status,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the TartMachine object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.3/pkg/reconcile
func (r *TartMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var machine infrastructurev1alpha1.TartMachine
	if err := r.Get(ctx, req.NamespacedName, &machine); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if machine.Status.HostRef != nil {
		return ctrl.Result{}, nil
	}

	host, err := r.reserveAvailableHost(ctx, &machine)
	if err != nil {
		return ctrl.Result{}, err
	}
	if host == nil {
		original := machine.DeepCopy()
		apimeta.SetStatusCondition(&machine.Status.Conditions, metav1.Condition{
			Type:               "HostReserved",
			Status:             metav1.ConditionFalse,
			Reason:             "NoAvailableHost",
			Message:            "No available TartHost exists",
			ObservedGeneration: machine.Generation,
		})
		machine.Status.ObservedGeneration = machine.Generation
		return ctrl.Result{}, r.Status().Patch(ctx, &machine, client.MergeFrom(original))
	}

	original := machine.DeepCopy()
	now := metav1.Now()
	expiresAt := metav1.NewTime(now.Add(bootstrapTokenTTL))
	machine.Status.HostRef = tartHostRef(host)
	// ワンタイムトークンは Bootstrap Data の推測困難な URL とシングルショット配信の基礎になります。
	machine.Status.BootstrapToken = string(uuid.NewUUID())
	machine.Status.ProvisioningStartTime = &now
	machine.Status.TokenExpiresAt = &expiresAt
	machine.Status.ObservedGeneration = machine.Generation
	apimeta.SetStatusCondition(&machine.Status.Conditions, metav1.Condition{
		Type:               "HostReserved",
		Status:             metav1.ConditionTrue,
		Reason:             "HostReserved",
		Message:            fmt.Sprintf("Reserved TartHost %s/%s", host.Namespace, host.Name),
		ObservedGeneration: machine.Generation,
	})
	if err := r.Status().Patch(ctx, &machine, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("TartMachine に TartHost を割り当てました", "machine", req.String(), "host", client.ObjectKeyFromObject(host).String())
	return ctrl.Result{}, nil
}

func (r *TartMachineReconciler) reserveAvailableHost(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) (*infrastructurev1alpha1.TartHost, error) {
	var hosts infrastructurev1alpha1.TartHostList
	if err := r.List(ctx, &hosts, client.InNamespace(machine.Namespace)); err != nil {
		return nil, err
	}

	for i := range hosts.Items {
		host := &hosts.Items[i]
		if host.Status.State != infrastructurev1alpha1.TartHostStateAvailable || host.Status.MachineRef != nil {
			continue
		}

		original := host.DeepCopy()
		host.Status.State = infrastructurev1alpha1.TartHostStateReserved
		host.Status.MachineRef = tartMachineRef(machine)
		host.Status.ObservedGeneration = host.Generation
		apimeta.SetStatusCondition(&host.Status.Conditions, metav1.Condition{
			Type:               "Available",
			Status:             metav1.ConditionFalse,
			Reason:             "Reserved",
			Message:            fmt.Sprintf("Reserved by TartMachine %s/%s", machine.Namespace, machine.Name),
			ObservedGeneration: host.Generation,
		})
		if err := r.Status().Patch(ctx, host, client.MergeFrom(original)); err != nil {
			return nil, err
		}
		return host, nil
	}

	return nil, nil
}

func tartHostRef(host *infrastructurev1alpha1.TartHost) *corev1.ObjectReference {
	return &corev1.ObjectReference{
		APIVersion: infrastructurev1alpha1.GroupVersion.String(),
		Kind:       "TartHost",
		Namespace:  host.Namespace,
		Name:       host.Name,
		UID:        host.UID,
	}
}

func tartMachineRef(machine *infrastructurev1alpha1.TartMachine) *corev1.ObjectReference {
	return &corev1.ObjectReference{
		APIVersion: infrastructurev1alpha1.GroupVersion.String(),
		Kind:       "TartMachine",
		Namespace:  machine.Namespace,
		Name:       machine.Name,
		UID:        machine.UID,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *TartMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.TartMachine{}).
		Named("tartmachine").
		Complete(r)
}
