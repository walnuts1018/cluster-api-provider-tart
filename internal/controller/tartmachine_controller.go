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
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	onetimetoken "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/onetime_token"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/wol"
)

// TartMachineReconciler reconciles a TartMachine object
type TartMachineReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	WakeOnLANSender WakeOnLANSender
}

// WakeOnLANSender はホストの電源投入を行うための境界です。
type WakeOnLANSender interface {
	Send(macAddress string) error
}

const bootstrapTokenTTL = 10 * time.Minute

const tartMachineHostCleanupFinalizer = "infrastructure.cluster.x-k8s.io/tartmachine-host-cleanup"

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

	if !machine.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &machine)
	}

	if err := r.ensureFinalizer(ctx, &machine); err != nil {
		return ctrl.Result{}, err
	}

	// HostRef が設定済みの場合はホストの Provisioning 状態を確認し、
	// 未完了であれば WoL 再送信と状態遷移を行います（前回 reconcile の途中失敗からの再試行）。
	if machine.Status.HostRef != nil {
		return r.ensureHostProvisioning(ctx, &machine)
	}

	// ワンタイムトークンは Bootstrap Data の推測困難な URL とシングルショット配信の基礎になります。
	token, err := onetimetoken.New()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to generate bootstrap token: %w", err)
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

	// ホスト予約直後に machine.Status.HostRef を書き込み、以降の手順で失敗しても
	// 再 reconcile 時に同じホストを使用できるようにします。
	original := machine.DeepCopy()
	now := metav1.Now()
	expiresAt := metav1.NewTime(now.Add(bootstrapTokenTTL))
	machine.Status.HostRef = tartHostRef(host)
	machine.Status.BootstrapToken = token.String()
	machine.Status.ProvisioningStartTime = &now
	machine.Status.TokenExpiresAt = &expiresAt
	machine.Status.ObservedGeneration = machine.Generation
	apimeta.SetStatusCondition(&machine.Status.Conditions, metav1.Condition{
		Type:               "HostReserved",
		Status:             metav1.ConditionTrue,
		Reason:             "ProvisioningStarted",
		Message:            fmt.Sprintf("Reserved TartHost %s/%s and sent Wake-on-LAN", host.Namespace, host.Name),
		ObservedGeneration: machine.Generation,
	})
	if err := r.Status().Patch(ctx, &machine, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.wakeOnLANSender().Send(bootMACAddress(host)); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to send wake-on-lan: %w", err)
	}

	if err := r.markHostProvisioning(ctx, host); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Assigned TartHost to TartMachine", "machine", req.String(), "host", client.ObjectKeyFromObject(host).String())
	return ctrl.Result{}, nil
}

func (r *TartMachineReconciler) ensureFinalizer(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) error {
	if controllerutil.ContainsFinalizer(machine, tartMachineHostCleanupFinalizer) {
		return nil
	}

	original := machine.DeepCopy()
	controllerutil.AddFinalizer(machine, tartMachineHostCleanupFinalizer)
	return r.Patch(ctx, machine, client.MergeFrom(original))
}

func (r *TartMachineReconciler) reconcileDelete(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(machine, tartMachineHostCleanupFinalizer) {
		return ctrl.Result{}, nil
	}

	if err := r.releaseAssignedHost(ctx, machine); err != nil {
		return ctrl.Result{}, err
	}

	original := machine.DeepCopy()
	controllerutil.RemoveFinalizer(machine, tartMachineHostCleanupFinalizer)
	return ctrl.Result{}, r.Patch(ctx, machine, client.MergeFrom(original))
}

// ensureHostProvisioning は HostRef が設定済みの機械に対して、
// 割り当てホストが Provisioning 状態であることを保証します。
// 前回の reconcile が WoL 送信や状態遷移の途中で失敗した場合のリカバリに使用します。
func (r *TartMachineReconciler) ensureHostProvisioning(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) (ctrl.Result, error) {
	var host infrastructurev1alpha1.TartHost
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: machine.Status.HostRef.Namespace,
		Name:      machine.Status.HostRef.Name,
	}, &host); err != nil {
		return ctrl.Result{}, err
	}

	// すでに Provisioning 以降の状態であれば対応不要
	if host.Status.State == infrastructurev1alpha1.TartHostStateProvisioning ||
		host.Status.State == infrastructurev1alpha1.TartHostStateProvisioned {
		return ctrl.Result{}, nil
	}

	if err := r.wakeOnLANSender().Send(bootMACAddress(&host)); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to send wake-on-lan: %w", err)
	}
	return ctrl.Result{}, r.markHostProvisioning(ctx, &host)
}

func (r *TartMachineReconciler) wakeOnLANSender() WakeOnLANSender {
	if r.WakeOnLANSender != nil {
		return r.WakeOnLANSender
	}
	return wol.DefaultSender()
}

func (r *TartMachineReconciler) reserveAvailableHost(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) (*infrastructurev1alpha1.TartHost, error) {
	var hosts infrastructurev1alpha1.TartHostList
	if err := r.List(ctx, &hosts, client.InNamespace(machine.Namespace)); err != nil {
		return nil, err
	}

	for i := range hosts.Items {
		candidate := &hosts.Items[i]
		if candidate.Status.State != infrastructurev1alpha1.TartHostStateAvailable || candidate.Status.MachineRef != nil {
			continue
		}

		host := &infrastructurev1alpha1.TartHost{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(candidate), host); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		if host.Status.State != infrastructurev1alpha1.TartHostStateAvailable || host.Status.MachineRef != nil {
			continue
		}

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
		if err := r.Status().Update(ctx, host); err != nil {
			if apierrors.IsConflict(err) {
				continue
			}
			return nil, err
		}
		return host, nil
	}

	return nil, nil
}

func (r *TartMachineReconciler) markHostProvisioning(ctx context.Context, host *infrastructurev1alpha1.TartHost) error {
	original := host.DeepCopy()
	host.Status.State = infrastructurev1alpha1.TartHostStateProvisioning
	apimeta.SetStatusCondition(&host.Status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             metav1.ConditionFalse,
		Reason:             "Provisioning",
		Message:            "Host is provisioning a TartMachine after Wake-on-LAN",
		ObservedGeneration: host.Generation,
	})
	return r.Status().Patch(ctx, host, client.MergeFrom(original))
}

func (r *TartMachineReconciler) releaseAssignedHost(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) error {
	if machine.Status.HostRef == nil {
		return nil
	}

	var host infrastructurev1alpha1.TartHost
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: machine.Status.HostRef.Namespace,
		Name:      machine.Status.HostRef.Name,
	}, &host); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if host.Status.MachineRef == nil ||
		host.Status.MachineRef.Name != machine.Name ||
		host.Status.MachineRef.Namespace != machine.Namespace {
		return nil
	}

	original := host.DeepCopy()
	host.Status.State = infrastructurev1alpha1.TartHostStateAvailable
	host.Status.MachineRef = nil
	host.Status.ObservedGeneration = host.Generation
	apimeta.SetStatusCondition(&host.Status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             metav1.ConditionTrue,
		Reason:             "Released",
		Message:            fmt.Sprintf("Released from TartMachine %s/%s", machine.Namespace, machine.Name),
		ObservedGeneration: host.Generation,
	})
	return r.Status().Patch(ctx, &host, client.MergeFrom(original))
}

func bootMACAddress(host *infrastructurev1alpha1.TartHost) string {
	if host.Spec.BootMACAddress != "" {
		return host.Spec.BootMACAddress
	}
	return host.Spec.MACAddress
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
		// Available な TartHost 出現時に即座にマシン割り当てを行うため、
		// TartHost の状態変化を watch して未割当 TartMachine を再 reconcile します。
		Watches(
			&infrastructurev1alpha1.TartHost{},
			handler.EnqueueRequestsFromMapFunc(r.tartHostToUnassignedTartMachines),
		).
		Named("tartmachine").
		Complete(r)
}

// tartHostToUnassignedTartMachines は TartHost の変更を受け取り、
// 同 namespace の未割当 TartMachine を reconcile キューに積みます。
func (r *TartMachineReconciler) tartHostToUnassignedTartMachines(ctx context.Context, obj client.Object) []reconcile.Request {
	host, ok := obj.(*infrastructurev1alpha1.TartHost)
	if !ok {
		return nil
	}
	if host.Status.State != infrastructurev1alpha1.TartHostStateAvailable {
		return nil
	}

	var machines infrastructurev1alpha1.TartMachineList
	if err := r.List(ctx, &machines, client.InNamespace(host.Namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list unassigned TartMachines", "host", client.ObjectKeyFromObject(host).String())
		return nil
	}

	requests := make([]reconcile.Request, 0, len(machines.Items))
	for _, machine := range machines.Items {
		if machine.Status.HostRef == nil {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: machine.Namespace,
					Name:      machine.Name,
				},
			})
		}
	}
	return requests
}
