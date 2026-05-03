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
	applicationhost "github.com/walnuts1018/cluster-api-provider-tart/internal/application/host"
	applicationprovisioning "github.com/walnuts1018/cluster-api-provider-tart/internal/application/provisioning"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
	onetimetoken "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/onetime_token"
)

// TartMachineReconciler reconciles a TartMachine object
type TartMachineReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	HostService  applicationhost.Service
	Provisioning applicationprovisioning.Service
}

const bootstrapTokenTTL = 10 * time.Minute

const tartMachineHostCleanupFinalizer = "infrastructure.cluster.x-k8s.io/tartmachine-host-cleanup"

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines/finalizers,verbs=update
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
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
		// トークン期限が切れている場合は新しいトークンを発行し、WoL を再送信してリトライ
		if machine.Status.TokenExpiresAt != nil && time.Now().After(machine.Status.TokenExpiresAt.Time) {
			log.Info("Bootstrap token expired, regenerating token and retrying", "machine", client.ObjectKeyFromObject(&machine).String())
			newToken, err := onetimetoken.New()
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to generate bootstrap token for retry: %w", err)
			}
			original := machine.DeepCopy()
			now := metav1.Now()
			expiresAt := metav1.NewTime(now.Add(bootstrapTokenTTL))
			machine.Status.BootstrapToken = newToken.String()
			machine.Status.ProvisioningStartTime = &now
			machine.Status.TokenExpiresAt = &expiresAt
			apimeta.SetStatusCondition(&machine.Status.Conditions, metav1.Condition{
				Type:               "Provisioning",
				Status:             metav1.ConditionFalse,
				Reason:             "TokenExpired",
				Message:            "Bootstrap token expired, regenerating and retrying",
				ObservedGeneration: machine.Generation,
			})
			machine.Status.ObservedGeneration = machine.Generation
			if err := r.Status().Patch(ctx, &machine, client.MergeFrom(original)); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.provisioningService().Ensure(ctx, &machine); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
		}

		if err := r.provisioningService().Ensure(ctx, &machine); err != nil {
			return ctrl.Result{}, err
		}

		// BootstrapToken が消費された（メタデータ配信が完了した）場合、
		// TartMachine を Ready に遷移させ、TartHost を Provisioned にします。
		if machine.Status.BootstrapToken == "" {
			original := machine.DeepCopy()
			machine.Status.Ready = true
			machine.Status.ObservedGeneration = machine.Generation
			if err := r.Status().Patch(ctx, &machine, client.MergeFrom(original)); err != nil {
				return ctrl.Result{}, err
			}

			host, err := r.hostService().GetAssigned(ctx, &machine)
			if err != nil {
				return ctrl.Result{}, err
			}
			if host != nil {
				if err := r.hostService().MarkProvisioned(ctx, host); err != nil {
					return ctrl.Result{}, err
				}
			}
		}

		return ctrl.Result{}, nil
	}

	// ワンタイムトークンは Bootstrap Data の推測困難な URL とシングルショット配信の基礎になります。
	token, err := onetimetoken.New()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to generate bootstrap token: %w", err)
	}

	host, err := r.hostService().ReserveAvailable(ctx, &machine)
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
	machine.Status.HostRef = hostdomain.RefForHost(host)
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

	if err := r.provisioningService().Begin(ctx, host); err != nil {
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

	if err := r.hostService().ReleaseAssigned(ctx, machine); err != nil {
		return ctrl.Result{}, err
	}

	original := machine.DeepCopy()
	controllerutil.RemoveFinalizer(machine, tartMachineHostCleanupFinalizer)
	return ctrl.Result{}, r.Patch(ctx, machine, client.MergeFrom(original))
}

func (r *TartMachineReconciler) hostService() applicationhost.Service {
	if r.HostService != nil {
		return r.HostService
	}
	panic("HostService is not configured")
}

func (r *TartMachineReconciler) provisioningService() applicationprovisioning.Service {
	if r.Provisioning != nil {
		return r.Provisioning
	}
	panic("Provisioning service is not configured")
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
