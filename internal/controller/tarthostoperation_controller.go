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
	appupdate "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
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
	Scheme             *runtime.Scheme
	PowerOn            OperationPowerOnService
	PrepareBoot        OperationBootPreparationService
	HostPhase          OperationHostPhaseService
	Targets            OperationDriverTargetBuilder
	DriverCapabilities OperationDriverCapabilityObserver
	DriverPowerState   OperationDriverPowerStateObserver
	DriverBootState    OperationDriverBootStateObserver
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

// OperationBootPreparationService はPowerOn前に利用するboot transportを準備する。
type OperationBootPreparationService interface {
	PrepareBoot(
		context.Context,
		driverdomain.Name,
		driverdomain.HostTarget,
		operationdomain.ID,
		*driverdomain.BootTarget,
		applicationdriver.Invocation,
	) (driverdomain.BootTarget, error)
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
	// MarkHostCleaningForDeletion は削除ポリシーに応じたCleaning開始を記録する。
	MarkHostCleaningForDeletion(
		ctx context.Context,
		host *infrastructurev1beta1.TartHost,
		deletionPolicy infrastructurev1beta1.DeletionPolicy,
	) error
	// MarkHostRetained はData保持のCleaning完了を記録する。
	MarkHostRetained(ctx context.Context, host *infrastructurev1beta1.TartHost) error
	// MarkHostDetached はState/Data保持のCleaning完了を記録する。
	MarkHostDetached(ctx context.Context, host *infrastructurev1beta1.TartHost) error
}

// OperationDriverTargetBuilder はTartHostからdriver呼び出し対象を構築する。
type OperationDriverTargetBuilder interface {
	Build(context.Context, *infrastructurev1beta1.TartHost) (driverdomain.HostTarget, error)
}

// OperationDriverCapabilityObserver はHostごとのdriver capabilityを観測しStatusへ反映する。
type OperationDriverCapabilityObserver interface {
	ObserveAndPersist(
		context.Context,
		driverdomain.Name,
		driverdomain.HostTarget,
		*infrastructurev1beta1.TartHost,
		applicationdriver.Invocation,
	) error
}

// OperationDriverPowerStateObserver はHostごとのdriver power stateを観測しStatusへ反映する。
type OperationDriverPowerStateObserver interface {
	ObserveAndPersist(
		context.Context,
		driverdomain.Name,
		driverdomain.HostTarget,
		*infrastructurev1beta1.TartHost,
		applicationdriver.Invocation,
	) error
}

// OperationDriverBootStateObserver はHostごとのdriver boot stateを観測しStatusへ反映する。
type OperationDriverBootStateObserver interface {
	ObserveBootAndPersist(
		context.Context,
		driverdomain.Name,
		driverdomain.HostTarget,
		*infrastructurev1beta1.TartHost,
		applicationdriver.Invocation,
	) error
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

	kind, err := operationdomain.ParseKind(string(operation.Spec.Type))
	if err != nil {
		return ctrl.Result{}, err
	}
	var phase operationdomain.Phase
	if operation.Status.Phase != "" {
		phase, err = operationdomain.ParsePhase(string(operation.Status.Phase))
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	decision, err := operationdomain.DecideNextStep(operationdomain.WorkflowInput{
		Kind:     kind,
		Phase:    phase,
		Deadline: operation.Spec.Deadline.Time,
		Now:      time.Now(),
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	switch selected := decision.Step.(type) {
	case operationdomain.StepInitializePending:
		return ctrl.Result{}, r.transitionPhase(ctx, &operation, infrastructurev1beta1.TartHostOperationPhasePending)
	case operationdomain.StepPrepareBoot:
		return ctrl.Result{}, r.handlePending(ctx, &operation)
	case operationdomain.StepObserveActive:
		if err := r.observeActiveOperationDriverState(ctx, &operation); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: operationDeadlineRequeueInterval}, nil
	case operationdomain.StepAwaitMachineHealth:
		return ctrl.Result{RequeueAfter: operationDeadlineRequeueInterval}, nil
	case operationdomain.StepCompleteWipeAll:
		return ctrl.Result{}, r.handleWipeAllAwaitingHealth(ctx, &operation)
	case operationdomain.StepCompleteCleaning:
		return ctrl.Result{}, r.handleCleaningAwaitingHealth(ctx, &operation)
	case operationdomain.StepHandleTerminal:
		if err := r.handleTerminal(ctx, &operation, selected.Phase); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	case operationdomain.StepFailDeadlineExceeded:
		log.Info("TartHostOperation deadline exceeded",
			"operation", req.String(),
			"deadline", operation.Spec.Deadline.Time,
			"phase", operation.Status.Phase,
		)
		return ctrl.Result{}, r.handleDeadlineExceeded(ctx, &operation)
	case operationdomain.StepIgnore:
		log.V(4).Info("TartHostOperation in unhandled phase, skipping",
			"operation", req.String(),
			"phase", operation.Status.Phase,
		)
		return ctrl.Result{}, nil
	default:
		return ctrl.Result{}, fmt.Errorf("unknown TartHostOperation workflow step %T", selected)
	}
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

	operationID, err := operationdomain.ParseID(operation.Spec.OperationID)
	if err != nil {
		return fmt.Errorf("parse operation ID: %w", err)
	}

	target, err := r.driverTarget(ctx, host)
	if err != nil {
		return err
	}

	powerDriverName, err := driverdomain.ParseName(host.Spec.Management.PowerDriver)
	if err != nil {
		return fmt.Errorf("parse power driver name: %w", err)
	}
	bootDriver := host.Spec.Management.BootDriver
	if bootDriver == "" {
		bootDriver = host.Spec.Management.PowerDriver
	}
	bootDriverName, err := driverdomain.ParseName(bootDriver)
	if err != nil {
		return fmt.Errorf("parse boot driver name: %w", err)
	}
	invocation := applicationdriver.Invocation{
		OperationType: string(operation.Spec.Type),
		Phase:         "PreparingBoot",
		Rollback:      false,
	}
	if err := r.observeDriverCapabilities(ctx, host, powerDriverName, target, invocation); err != nil {
		log.Error(err, "Failed to observe TartHost driver capabilities",
			"host", client.ObjectKeyFromObject(host).String(),
			"driver", powerDriverName,
		)
		return fmt.Errorf("observe TartHost driver capabilities: %w", err)
	}
	if err := r.observeDriverPowerState(ctx, host, powerDriverName, target, invocation); err != nil {
		log.Error(err, "Failed to observe TartHost power state",
			"host", client.ObjectKeyFromObject(host).String(),
			"driver", powerDriverName,
		)
		return fmt.Errorf("observe TartHost power state: %w", err)
	}
	if err := r.observeDriverBootState(ctx, host, bootDriverName, target, invocation); err != nil {
		log.Error(err, "Failed to observe TartHost boot state",
			"host", client.ObjectKeyFromObject(host).String(),
			"driver", bootDriverName,
		)
		return fmt.Errorf("observe TartHost boot state: %w", err)
	}
	if err := r.prepareBoot(ctx, host, bootDriverName, target, operationID, invocation); err != nil {
		log.Error(err, "Failed to prepare TartHost boot transport",
			"host", client.ObjectKeyFromObject(host).String(),
			"driver", bootDriverName,
		)
		return fmt.Errorf("prepare TartHost boot transport: %w", err)
	}

	if err := r.PowerOn.PowerOn(
		ctx,
		powerDriverName,
		target,
		operationID,
		invocation,
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
	switch operation.Spec.Type {
	case infrastructurev1beta1.OperationTypeUpdate:
		markHostPhase = r.HostPhase.MarkHostUpdating
		targetHostPhase = infrastructurev1beta1.TartHostPhaseUpdating
	case infrastructurev1beta1.OperationTypeClean:
		policy, err := r.cleaningPolicy(ctx, operation)
		if err != nil {
			return err
		}
		markHostPhase = func(ctx context.Context, host *infrastructurev1beta1.TartHost) error {
			return r.HostPhase.MarkHostCleaningForDeletion(ctx, host, policy)
		}
		targetHostPhase = infrastructurev1beta1.TartHostPhaseCleaning
	case infrastructurev1beta1.OperationTypeWipeAll:
		markHostPhase = func(ctx context.Context, host *infrastructurev1beta1.TartHost) error {
			return r.HostPhase.MarkHostCleaningForDeletion(ctx, host, infrastructurev1beta1.DeletionPolicyWipeAll)
		}
		targetHostPhase = infrastructurev1beta1.TartHostPhaseCleaning
	default:
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

func (r *TartHostOperationReconciler) prepareBoot(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	driverName driverdomain.Name,
	target driverdomain.HostTarget,
	operationID operationdomain.ID,
	invocation applicationdriver.Invocation,
) error {
	if r.PrepareBoot == nil || host.Spec.Management.BootDriver != "redfish" {
		return nil
	}
	preferred, err := preferredBootTarget(host)
	if err != nil {
		return err
	}
	_, err = r.PrepareBoot.PrepareBoot(ctx, driverName, target, operationID, preferred, invocation)
	return err
}

func preferredBootTarget(host *infrastructurev1beta1.TartHost) (*driverdomain.BootTarget, error) {
	if host.Spec.Management.Redfish == nil {
		return nil, nil
	}
	switch host.Spec.Management.Redfish.PreferredBootTransport {
	case "":
		return nil, nil
	case infrastructurev1beta1.BootTransportRedfishHTTPBoot:
		target := driverdomain.BootTargetHTTP
		return &target, nil
	case infrastructurev1beta1.BootTransportRedfishPXE:
		target := driverdomain.BootTargetPXE
		return &target, nil
	case infrastructurev1beta1.BootTransportRedfishVirtualMedia:
		target := driverdomain.BootTargetVirtualMedia
		return &target, nil
	default:
		return nil, fmt.Errorf("unsupported Redfish preferred boot transport %q", host.Spec.Management.Redfish.PreferredBootTransport)
	}
}

func (r *TartHostOperationReconciler) driverTarget(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
) (driverdomain.HostTarget, error) {
	if r.Targets != nil {
		return r.Targets.Build(ctx, host)
	}
	bootMAC, err := driverdomain.ParseMACAddress(host.Spec.Identifiers.BootMACAddress)
	if err != nil {
		return driverdomain.HostTarget{}, fmt.Errorf("parse TartHost boot MAC address: %w", err)
	}
	return driverdomain.NewHostTarget(bootMAC), nil
}

func (r *TartHostOperationReconciler) observeDriverCapabilities(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	driverName driverdomain.Name,
	target driverdomain.HostTarget,
	invocation applicationdriver.Invocation,
) error {
	if r.DriverCapabilities == nil {
		return nil
	}
	return r.DriverCapabilities.ObserveAndPersist(ctx, driverName, target, host, invocation)
}

func (r *TartHostOperationReconciler) observeDriverPowerState(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	driverName driverdomain.Name,
	target driverdomain.HostTarget,
	invocation applicationdriver.Invocation,
) error {
	if r.DriverPowerState == nil {
		return nil
	}
	return r.DriverPowerState.ObserveAndPersist(ctx, driverName, target, host, invocation)
}

func (r *TartHostOperationReconciler) observeDriverBootState(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	driverName driverdomain.Name,
	target driverdomain.HostTarget,
	invocation applicationdriver.Invocation,
) error {
	if r.DriverBootState == nil || host.Spec.Management.BootDriver != "redfish" {
		return nil
	}
	return r.DriverBootState.ObserveBootAndPersist(ctx, driverName, target, host, invocation)
}

func (r *TartHostOperationReconciler) observeActiveOperationDriverState(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	if r.DriverBootState == nil && r.DriverPowerState == nil {
		return nil
	}
	host, err := r.getHost(ctx, operation)
	if err != nil {
		return err
	}
	target, err := r.driverTarget(ctx, host)
	if err != nil {
		return err
	}
	bootDriver := host.Spec.Management.BootDriver
	if bootDriver == "" {
		bootDriver = host.Spec.Management.PowerDriver
	}
	bootDriverName, err := driverdomain.ParseName(bootDriver)
	if err != nil {
		return fmt.Errorf("parse boot driver name: %w", err)
	}
	invocation := applicationdriver.Invocation{
		OperationType: string(operation.Spec.Type),
		Phase:         string(operation.Status.Phase),
		Rollback:      operation.Status.Phase == infrastructurev1beta1.TartHostOperationPhaseRollingBack,
	}
	powerDriverName, err := driverdomain.ParseName(host.Spec.Management.PowerDriver)
	if err != nil {
		return fmt.Errorf("parse power driver name: %w", err)
	}
	if err := r.observeDriverPowerState(ctx, host, powerDriverName, target, invocation); err != nil {
		return fmt.Errorf("observe TartHost power state: %w", err)
	}
	if err := r.observeDriverBootState(ctx, host, bootDriverName, target, invocation); err != nil {
		return fmt.Errorf("observe TartHost boot state: %w", err)
	}
	return nil
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

func (r *TartHostOperationReconciler) handleCleaningAwaitingHealth(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	host, err := r.getHost(ctx, operation)
	if err != nil {
		return err
	}
	policy, err := r.cleaningPolicy(ctx, operation)
	if err != nil {
		return err
	}
	switch policy {
	case infrastructurev1beta1.DeletionPolicyRetainData:
		if err := r.HostPhase.MarkHostRetained(ctx, host); err != nil {
			return fmt.Errorf("mark TartHost retained after Cleaning: %w", err)
		}
	case infrastructurev1beta1.DeletionPolicyRetainState:
		if err := r.HostPhase.MarkHostDetached(ctx, host); err != nil {
			return fmt.Errorf("mark TartHost detached after Cleaning: %w", err)
		}
	default:
		return fmt.Errorf("unsupported cleaning policy %q for Clean operation", policy)
	}
	return r.transitionPhase(ctx, operation, infrastructurev1beta1.TartHostOperationPhaseSucceeded)
}

func (r *TartHostOperationReconciler) cleaningPolicy(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) (infrastructurev1beta1.DeletionPolicy, error) {
	if operation.Spec.Type == infrastructurev1beta1.OperationTypeWipeAll {
		return infrastructurev1beta1.DeletionPolicyWipeAll, nil
	}
	if operation.Spec.MachineRef == nil {
		return "", fmt.Errorf("machineRef is required for Clean operation")
	}
	machine := &infrastructurev1beta1.TartMachine{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: operation.Spec.MachineRef.Namespace,
		Name:      operation.Spec.MachineRef.Name,
	}, machine); err != nil {
		return "", fmt.Errorf("get TartMachine for Cleaning policy: %w", err)
	}
	if machine.UID != operation.Spec.MachineRef.UID {
		return "", fmt.Errorf("TartMachine UID mismatch: expected %s, got %s", operation.Spec.MachineRef.UID, machine.UID)
	}
	return machine.Spec.DeletionPolicy, nil
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
	if operation.Spec.Type == infrastructurev1beta1.OperationTypeUpdate {
		return r.transitionUpdateFailurePhase(
			ctx,
			operation,
			operation.Status.Phase,
			infrastructurev1beta1.TartHostOperationPhaseFailed,
		)
	}
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
		return r.transitionUpdateFailurePhase(
			ctx,
			operation,
			infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
			infrastructurev1beta1.TartHostOperationPhaseRollingBack,
		)
	case infrastructurev1beta1.TartHostOperationPhaseRollingBack:
		return r.transitionUpdateFailurePhase(
			ctx,
			operation,
			infrastructurev1beta1.TartHostOperationPhaseRollingBack,
			infrastructurev1beta1.TartHostOperationPhaseRecoveryRequired,
		)
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
		appupdate.SetUpdateFailureCondition(
			&current.Status,
			current.Generation,
			infrastructurev1beta1.TartHostOperationPhaseBootTrial,
			current.Status.Phase,
		)
		if current.Status.ObservedGeneration < current.Generation {
			current.Status.ObservedGeneration = current.Generation
		}
		return r.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

func (r *TartHostOperationReconciler) transitionUpdateFailurePhase(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	failedPhase infrastructurev1beta1.TartHostOperationPhase,
	target infrastructurev1beta1.TartHostOperationPhase,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHostOperation{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(operation), current); err != nil {
			return err
		}
		original := current.DeepCopy()
		current.Status.Phase = target
		appupdate.UpdateFailureCondition(&current.Status, current.Generation, failedPhase, target)
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
