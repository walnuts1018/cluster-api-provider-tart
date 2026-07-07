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
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/internal/application/driver"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
	inplaceupdatedomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/inplaceupdate"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
	slotdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/slot"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/telemetry"
)

// TartHostOperationReconciler は TartHostOperation の Phase を進める。
// Phase の詳細なビジネスロジック（書き込みや検証）は Agent が行うため、
// このControllerはPreparingBootへの遷移とAwaitingHealth判定に集中する。
type TartHostOperationReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	PowerOn   OperationPowerOnService
	HostPhase OperationHostPhaseService
}

// OperationPowerOnService はOperationのPreparingBootフェーズでWoLを発火する。
type OperationPowerOnService interface {
	PowerOn(
		context.Context,
		driverdomain.Name,
		driverdomain.HostTarget,
		operationdomain.ID,
		applicationdriver.Invocation,
	) error
}

// OperationHostPhaseService はTartHostのPhaseをOperation結果に応じて更新する。
type OperationHostPhaseService interface {
	// MarkHostProvisioning はHostをProvisioningフェーズに移行する。
	MarkHostProvisioning(ctx context.Context, host *infrastructurev1beta1.TartHost) error
	// MarkHostUpdating はHostをUpdatingフェーズに移行する。
	MarkHostUpdating(ctx context.Context, host *infrastructurev1beta1.TartHost) error
	// MarkHostProvisioned はHostをProvisionedフェーズに移行する。
	MarkHostProvisioned(ctx context.Context, host *infrastructurev1beta1.TartHost) error
	// MarkHostRecoveryRequired はHostをRecoveryRequiredフェーズに移行する。
	MarkHostRecoveryRequired(ctx context.Context, host *infrastructurev1beta1.TartHost) error
	// MarkHostAvailable はHostをAvailableに戻す（ConsumerRefを除去）。
	MarkHostAvailable(ctx context.Context, host *infrastructurev1beta1.TartHost) error
}

const (
	// operationDeadlineRequeueInterval はDeadline超過を確認する間隔。
	operationDeadlineRequeueInterval = 1 * time.Minute
)

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthostoperations,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthostoperations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts/status,verbs=get;update;patch

func (r *TartHostOperationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	ctx, span := telemetry.Tracer.Start(ctx, "TartHostOperation.Reconcile")
	span.SetAttributes(
		attribute.String("kubernetes.resource.name", req.Name),
		attribute.String("kubernetes.resource.namespace", req.Namespace),
	)
	defer span.End()

	var operation infrastructurev1beta1.TartHostOperation
	if err := r.Get(ctx, req.NamespacedName, &operation); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// terminal Phaseは変更しない
	phase, err := operationdomain.ParsePhase(string(operation.Status.Phase))
	if err == nil && phase.Terminal() {
		if err := r.handleTerminal(ctx, &operation, phase); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Deadline超過時はOperation種別とphaseに応じて失敗状態へ遷移する。
	if !operation.Spec.Deadline.IsZero() && time.Now().After(operation.Spec.Deadline.Time) {
		log.Info("TartHostOperation deadline exceeded",
			"operation", req.String(),
			"deadline", operation.Spec.Deadline.Time,
			"phase", operation.Status.Phase,
		)
		return ctrl.Result{}, r.handleDeadlineExceeded(ctx, &operation)
	}

	switch operation.Status.Phase {
	case "":
		// 初回Reconcile: PendingへPhaseを設定する
		return ctrl.Result{}, r.transitionPhase(ctx, &operation, infrastructurev1beta1.TartHostOperationPhasePending)

	case infrastructurev1beta1.TartHostOperationPhasePending:
		// Pending → PreparingBoot: WoLを発火してHostをOperation種別に対応するPhaseへ移行
		return ctrl.Result{}, r.handlePending(ctx, &operation)

	case infrastructurev1beta1.TartHostOperationPhasePreparingBoot,
		infrastructurev1beta1.TartHostOperationPhaseWaitingForAgent,
		infrastructurev1beta1.TartHostOperationPhaseWriting,
		infrastructurev1beta1.TartHostOperationPhaseVerifying,
		infrastructurev1beta1.TartHostOperationPhaseBootTrial:
		// これらのPhaseはAgent/Driverがphase報告経由で進める。
		// OperationがDeadline内かどうかをrequeue間隔で確認する。
		return ctrl.Result{RequeueAfter: operationDeadlineRequeueInterval}, nil

	case infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth:
		// WipeAll OperationはNodeRefなしでHostをAvailableに戻す特殊処理を行う。
		// Provision/Update OperationはTartMachineV1Beta1ReconcilerがNode健全性を確認して完了させる。
		if operation.Spec.Type == infrastructurev1beta1.OperationTypeWipeAll {
			return ctrl.Result{}, r.handleWipeAllAwaitingHealth(ctx, &operation)
		}
		return ctrl.Result{RequeueAfter: operationDeadlineRequeueInterval}, nil

	default:
		log.V(4).Info("TartHostOperation in unhandled phase, skipping",
			"operation", req.String(),
			"phase", operation.Status.Phase,
		)
	}

	return ctrl.Result{}, nil
}

func (r *TartHostOperationReconciler) handleTerminal(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	phase operationdomain.Phase,
) error {
	if operation.Spec.Type != infrastructurev1beta1.OperationTypeUpdate {
		return nil
	}
	host, err := r.getHost(ctx, operation)
	if err != nil {
		return err
	}
	switch phase {
	case operationdomain.PhaseSucceeded, operationdomain.PhaseFailed:
		return r.HostPhase.MarkHostProvisioned(ctx, host)
	case operationdomain.PhaseRecoveryRequired:
		return r.HostPhase.MarkHostRecoveryRequired(ctx, host)
	default:
		return nil
	}
}

func (r *TartHostOperationReconciler) handlePending(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	log := logf.FromContext(ctx)

	host, err := r.getHost(ctx, operation)
	if err != nil {
		return err
	}

	bootMAC, err := driverdomain.ParseMACAddress(host.Spec.Identifiers.BootMACAddress)
	if err != nil {
		return fmt.Errorf("parse TartHost boot MAC address: %w", err)
	}
	operationID, err := operationdomain.ParseID(operation.Spec.OperationID)
	if err != nil {
		return fmt.Errorf("parse operation ID: %w", err)
	}

	powerDriverName, err := driverdomain.ParseName(host.Spec.Management.PowerDriver)
	if err != nil {
		return fmt.Errorf("parse power driver name: %w", err)
	}

	if err := r.PowerOn.PowerOn(
		ctx,
		powerDriverName,
		driverdomain.NewHostTarget(bootMAC),
		operationID,
		applicationdriver.Invocation{
			OperationType: string(operation.Spec.Type),
			Phase:         "PreparingBoot",
			Rollback:      false,
		},
	); err != nil {
		log.Error(err, "Failed to power on TartHost for Operation",
			"operation", client.ObjectKeyFromObject(operation).String(),
			"host", operation.Spec.HostRef.Name,
			"driver", host.Spec.Management.PowerDriver,
		)
		return fmt.Errorf("power on TartHost: %w", err)
	}

	// Hostのフェーズ更新とOperation Phase遷移を行う。
	var markHostPhase func(context.Context, *infrastructurev1beta1.TartHost) error
	targetHostPhase := infrastructurev1beta1.TartHostPhaseProvisioning
	if operation.Spec.Type == infrastructurev1beta1.OperationTypeUpdate {
		markHostPhase = r.HostPhase.MarkHostUpdating
		targetHostPhase = infrastructurev1beta1.TartHostPhaseUpdating
	} else {
		markHostPhase = r.HostPhase.MarkHostProvisioning
	}
	if err := markHostPhase(ctx, host); err != nil {
		log.Error(err, "Failed to mark TartHost for Operation",
			"host", client.ObjectKeyFromObject(host).String(),
			"phase", targetHostPhase,
		)
		return err
	}

	return r.transitionPhase(ctx, operation, infrastructurev1beta1.TartHostOperationPhasePreparingBoot)
}

func (r *TartHostOperationReconciler) handleWipeAllAwaitingHealth(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	// WipeAll完了後はHostをAvailableに戻す。ConsumerRefも除去する。
	host, err := r.getHost(ctx, operation)
	if err != nil {
		return err
	}

	if err := r.HostPhase.MarkHostAvailable(ctx, host); err != nil {
		return fmt.Errorf("mark TartHost available after WipeAll: %w", err)
	}

	return r.transitionPhase(ctx, operation, infrastructurev1beta1.TartHostOperationPhaseSucceeded)
}

func (r *TartHostOperationReconciler) getHost(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) (*infrastructurev1beta1.TartHost, error) {
	host := &infrastructurev1beta1.TartHost{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: operation.Spec.HostRef.Namespace,
		Name:      operation.Spec.HostRef.Name,
	}, host); err != nil {
		return nil, fmt.Errorf("get TartHost for Operation: %w", err)
	}
	if host.UID != operation.Spec.HostRef.UID {
		return nil, fmt.Errorf("TartHost UID mismatch: expected %s, got %s", operation.Spec.HostRef.UID, host.UID)
	}
	return host, nil
}

func (r *TartHostOperationReconciler) transitionPhase(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	target infrastructurev1beta1.TartHostOperationPhase,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHostOperation{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(operation), current); err != nil {
			return err
		}
		original := current.DeepCopy()
		current.Status.Phase = target
		if current.Status.ObservedGeneration < current.Generation {
			current.Status.ObservedGeneration = current.Generation
		}
		return r.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

func (r *TartHostOperationReconciler) markFailed(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	return r.transitionPhase(ctx, operation, infrastructurev1beta1.TartHostOperationPhaseFailed)
}

func (r *TartHostOperationReconciler) handleDeadlineExceeded(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	if operation.Spec.Type != infrastructurev1beta1.OperationTypeUpdate {
		return r.markFailed(ctx, operation)
	}

	switch operation.Status.Phase {
	case infrastructurev1beta1.TartHostOperationPhaseBootTrial:
		return r.handleBootTrialDeadlineExceeded(ctx, operation)
	case infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth:
		return r.transitionPhase(ctx, operation, infrastructurev1beta1.TartHostOperationPhaseRollingBack)
	case infrastructurev1beta1.TartHostOperationPhaseRollingBack:
		return r.transitionPhase(ctx, operation, infrastructurev1beta1.TartHostOperationPhaseRecoveryRequired)
	default:
		return r.markFailed(ctx, operation)
	}
}

func (r *TartHostOperationReconciler) handleBootTrialDeadlineExceeded(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	targetSlot, err := slotdomain.Parse(string(operation.Spec.TargetSlot))
	if err != nil {
		return err
	}
	activeSlot, err := targetSlot.Inactive()
	if err != nil {
		return err
	}
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHostOperation{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(operation), current); err != nil {
			return err
		}
		decision, err := inplaceupdatedomain.Transition(inplaceupdatedomain.State{
			Phase:      operationdomain.PhaseBootTrial,
			ActiveSlot: activeSlot,
			TargetSlot: targetSlot,
			Attempt:    current.Status.Attempt,
		}, inplaceupdatedomain.EventBootFailed)
		if err != nil {
			return err
		}
		original := current.DeepCopy()
		current.Status.Attempt = decision.Attempt
		current.Status.Phase = infrastructurev1beta1.TartHostOperationPhase(decision.Phase)
		if current.Status.ObservedGeneration < current.Generation {
			current.Status.ObservedGeneration = current.Generation
		}
		return r.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

func (r *TartHostOperationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1beta1.TartHostOperation{}).
		// TartHostの変更でPending Operationを再reconcileする
		Watches(
			&infrastructurev1beta1.TartHost{},
			handler.EnqueueRequestsFromMapFunc(r.hostToActiveOperations),
		).
		Named("tarthostoperation").
		Complete(r)
}

// hostToActiveOperations はTartHostの変化を対象にしたOperationのReconcileに変換する。
func (r *TartHostOperationReconciler) hostToActiveOperations(ctx context.Context, obj client.Object) []reconcile.Request {
	host, ok := obj.(*infrastructurev1beta1.TartHost)
	if !ok {
		return nil
	}

	var operations infrastructurev1beta1.TartHostOperationList
	if err := r.List(ctx, &operations, client.InNamespace(host.Namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to list TartHostOperations for TartHost",
			"host", client.ObjectKeyFromObject(host).String(),
		)
		return nil
	}

	var requests []reconcile.Request
	for _, op := range operations.Items {
		if op.Spec.HostRef.Name != host.Name || op.Spec.HostRef.Namespace != host.Namespace {
			continue
		}
		phase, err := operationdomain.ParsePhase(string(op.Status.Phase))
		if err != nil || phase.Terminal() {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: op.Namespace,
				Name:      op.Name,
			},
		})
	}
	return requests
}
