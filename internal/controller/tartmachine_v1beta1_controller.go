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
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	appprovisioning "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning"
	appupdate "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
	applicationallocation "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineallocation"
	applicationhealth "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinehealth"
	allocationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/allocation"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinelifecycle"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/telemetry"
)

type HostReferenceService interface {
	EnsureMachineHostReference(
		context.Context,
		*infrastructurev1beta1.TartMachine,
	) (allocationdomain.ReferenceResult, error)
}

// ProvisionWorkflow はv1beta1 TartMachineの初期Provisioningを進める。
type ProvisionWorkflow interface {
	Start(
		ctx context.Context,
		machine *infrastructurev1beta1.TartMachine,
		planDigest string,
	) (appprovisioning.StartResult, error)
	CompleteProvisioning(
		ctx context.Context,
		host *infrastructurev1beta1.TartHost,
		operation *infrastructurev1beta1.TartHostOperation,
	) error
}

type TartMachineV1Beta1Reconciler struct {
	client.Client
	HostReferences HostReferenceService
	NodeHealth     NodeHealthObserver
	Provisioner    ProvisionWorkflow
	Cleaner        CleaningWorkflow
	Recorder       record.EventRecorder
}

type CleaningWorkflow interface {
	StartCleaning(
		ctx context.Context,
		machine *infrastructurev1beta1.TartMachine,
		host *infrastructurev1beta1.TartHost,
	) (*infrastructurev1beta1.TartHostOperation, error)
}

const tartMachineCleanupFinalizer = "infrastructure.cluster.x-k8s.io/tartmachine-cleanup"

type NodeHealthObserver interface {
	Observe(
		context.Context,
		*infrastructurev1beta1.TartMachine,
	) (machinehealthdomain.NodeObservation, bool, error)
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines/finalizers,verbs=update
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthostoperations,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *TartMachineV1Beta1Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	ctx, span := telemetry.Tracer.Start(ctx, "TartMachineV1Beta1.Reconcile")
	span.SetAttributes(
		attribute.String("kubernetes.resource.name", req.Name),
		attribute.String("kubernetes.resource.namespace", req.Namespace),
	)
	defer span.End()

	machine := &infrastructurev1beta1.TartMachine{}
	if err := r.Get(ctx, req.NamespacedName, machine); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get TartMachine: %w", err)
	}
	if !machine.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.reconcileDelete(ctx, machine)
	}
	if err := r.ensureFinalizer(ctx, machine); err != nil {
		return ctrl.Result{}, err
	}

	machineState := tartMachineState(machine)
	switch machinelifecycledomain.DecideMachine(machineState) {
	case machinelifecycledomain.CommandObserveProvisionedMachine{}:
		updateHandled, err := r.reconcileUpdateOperation(ctx, machine)
		if err != nil {
			return ctrl.Result{}, err
		}
		if updateHandled {
			return ctrl.Result{}, nil
		}
		if err := r.reconcileNodeHealth(ctx, machine); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	case machinelifecycledomain.CommandEnsureProvisionReference{}:
		// 未ProvisionedのMachineだけがHost割当とProvision Operation作成を進める。
	default:
		return ctrl.Result{}, fmt.Errorf("unknown TartMachine command")
	}

	// HostRef修復と一貫性チェック
	result, err := r.HostReferences.EnsureMachineHostReference(ctx, machine)
	if errors.Is(err, allocationdomain.ErrConflict) {
		original := machine.DeepCopy()
		machine.Status = applicationallocation.StatusWithAllocationConflict(machine, err.Error())
		if patchErr := r.Status().Patch(ctx, machine, client.MergeFrom(original)); patchErr != nil {
			return ctrl.Result{}, fmt.Errorf("set TartMachine AllocationConflict condition: %w", patchErr)
		}
		log.Info("TartMachine allocation conflict detected", "machine", req.String(), "error", err.Error())
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure TartMachine host reference: %w", err)
	}
	if result == allocationdomain.ReferenceRepaired {
		log.Info("Repaired TartMachine host reference", "machine", req.String())
	}

	switch machinelifecycledomain.DecideProvision(tartMachineState(machine)) {
	case machinelifecycledomain.CommandStartProvision{}:
		err = r.reconcileProvisionStart(ctx, machine)
	case machinelifecycledomain.CommandResumeProvisionOperation{}:
		err = r.reconcileOperation(ctx, machine)
	default:
		return ctrl.Result{}, fmt.Errorf("unknown TartMachine provision command")
	}

	if err != nil {
		return ctrl.Result{}, err
	}

	// 常にNodeHealthを観察し、Provisionedへの遷移やProviderIDMismatchの検出を行う
	if err := r.reconcileNodeHealth(ctx, machine); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *TartMachineV1Beta1Reconciler) ensureFinalizer(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	if controllerutil.ContainsFinalizer(machine, tartMachineCleanupFinalizer) {
		return nil
	}
	original := machine.DeepCopy()
	controllerutil.AddFinalizer(machine, tartMachineCleanupFinalizer)
	if err := r.Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("add TartMachine cleanup finalizer: %w", err)
	}
	return nil
}

func (r *TartMachineV1Beta1Reconciler) reconcileDelete(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	if !controllerutil.ContainsFinalizer(machine, tartMachineCleanupFinalizer) {
		return nil
	}
	if machine.Status.HostRef == nil {
		return r.removeFinalizer(ctx, machine)
	}

	host := &infrastructurev1beta1.TartHost{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: machine.Status.HostRef.Namespace,
		Name:      machine.Status.HostRef.Name,
	}, host); err != nil {
		if apierrors.IsNotFound(err) {
			return r.removeFinalizer(ctx, machine)
		}
		return fmt.Errorf("get TartHost for delete reconcile: %w", err)
	}
	if host.UID != machine.Status.HostRef.UID {
		return r.removeFinalizer(ctx, machine)
	}

	if machine.Status.OperationRef == nil {
		if r.Cleaner == nil {
			return fmt.Errorf("start Cleaning operation: Cleaner is not configured")
		}
		operation, err := r.Cleaner.StartCleaning(ctx, machine, host)
		if err != nil {
			return err
		}
		original := machine.DeepCopy()
		machine.Status.OperationRef = &infrastructurev1beta1.ResourceReference{
			Namespace: operation.Namespace,
			Name:      operation.Name,
			UID:       operation.UID,
		}
		if err := r.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
			return fmt.Errorf("persist Cleaning operation reference: %w", err)
		}
		return nil
	}

	operation := &infrastructurev1beta1.TartHostOperation{}
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: machine.Status.OperationRef.Namespace,
		Name:      machine.Status.OperationRef.Name,
	}, operation); err != nil {
		if apierrors.IsNotFound(err) {
			original := machine.DeepCopy()
			machine.Status.OperationRef = nil
			if patchErr := r.Status().Patch(ctx, machine, client.MergeFrom(original)); patchErr != nil {
				return fmt.Errorf("clear missing Cleaning operation reference: %w", patchErr)
			}
			return nil
		}
		return fmt.Errorf("get Cleaning operation: %w", err)
	}
	if operation.UID != machine.Status.OperationRef.UID {
		original := machine.DeepCopy()
		machine.Status.OperationRef = nil
		if err := r.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
			return fmt.Errorf("clear mismatched Cleaning operation reference: %w", err)
		}
		return nil
	}
	phase, err := operationdomain.ParsePhase(string(operation.Status.Phase))
	if err != nil {
		return fmt.Errorf("parse Cleaning operation phase: %w", err)
	}
	if !phase.Terminal() {
		return nil
	}
	if phase != operationdomain.PhaseSucceeded {
		return fmt.Errorf("Cleaning operation finished in %s", phase)
	}
	return r.removeFinalizer(ctx, machine)
}

func (r *TartMachineV1Beta1Reconciler) removeFinalizer(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	original := machine.DeepCopy()
	controllerutil.RemoveFinalizer(machine, tartMachineCleanupFinalizer)
	if err := r.Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("remove TartMachine cleanup finalizer: %w", err)
	}
	return nil
}

func (r *TartMachineV1Beta1Reconciler) reconcileUpdateOperation(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (bool, error) {
	if machine.Status.OperationRef == nil {
		return false, nil
	}
	operation := &infrastructurev1beta1.TartHostOperation{}
	key := client.ObjectKey{
		Namespace: machine.Status.OperationRef.Namespace,
		Name:      machine.Status.OperationRef.Name,
	}
	if err := r.Get(ctx, key, operation); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get Update TartHostOperation: %w", err)
	}
	if operation.UID != machine.Status.OperationRef.UID ||
		operation.Spec.Type != infrastructurev1beta1.OperationTypeUpdate {
		return false, nil
	}

	command, err := machinelifecycleOperationCommand(true, operation)
	if err != nil {
		return false, fmt.Errorf("decide Update TartHostOperation outcome: %w", err)
	}
	updateTerminal, ok := command.(machinelifecycledomain.CommandApplyUpdateTerminal)
	if !ok {
		return false, nil
	}
	if err := r.applyUpdateTerminal(ctx, machine, operation, updateTerminal.Outcome); err != nil {
		return false, err
	}
	return true, nil
}

func (r *TartMachineV1Beta1Reconciler) applyUpdateTerminal(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
	outcome machinelifecycledomain.UpdateOutcome,
) error {
	original := machine.DeepCopy()
	switch outcome {
	case machinelifecycledomain.UpdateOutcomeSucceeded:
		machine.Status = appupdate.StatusWithUpdateSucceeded(machine, operation)
		appupdate.RecordUpdateOutcome(ctx, operation, "succeeded")
	case machinelifecycledomain.UpdateOutcomeRolledBack:
		machine.Status = appupdate.StatusWithUpdateRolledBack(machine, operation)
		appupdate.RecordUpdateOutcome(ctx, operation, "failed")
	case machinelifecycledomain.UpdateOutcomeRecoveryRequired:
		machine.Status = appupdate.StatusWithUpdateRecoveryRequired(machine, operation)
		appupdate.RecordUpdateOutcome(ctx, operation, "recovery_required")
	default:
		return fmt.Errorf("unknown TartMachine update outcome: %q", outcome)
	}
	if err := r.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("set TartMachine Update status: %w", err)
	}
	if outcome == machinelifecycledomain.UpdateOutcomeRolledBack ||
		outcome == machinelifecycledomain.UpdateOutcomeRecoveryRequired {
		trace.SpanFromContext(ctx).SetAttributes(appupdate.FailureTraceAttributes(operation)...)
		r.recordUpdateFailureEvent(machine, operation)
	}
	return nil
}

// isProvisioned はTartMachineがすでにProvisioned済みかどうかを返す。
func isProvisioned(machine *infrastructurev1beta1.TartMachine) bool {
	return machine.Status.Initialization.Provisioned != nil && *machine.Status.Initialization.Provisioned
}

// reconcileProvisionStart はHost予約とProvision Operation作成を再開可能な形で開始する。
// HostRefだけが修復済みの場合も呼び出し、OperationRefの保存前に停止した処理を再開する。
func (r *TartMachineV1Beta1Reconciler) reconcileProvisionStart(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	log := logf.FromContext(ctx)

	if r.Provisioner == nil {
		log.V(4).Info("Provisioner not configured, skipping provisioning", "machine", client.ObjectKeyFromObject(machine).String())
		return nil
	}

	// CAPI BootstrapSecretが用意されていない場合は待機する
	// Bootstrap Secretは、CAPIのMachineオブジェクトが指すSecretに入っている
	bootstrapReady, err := r.isBootstrapReady(ctx, machine)
	if err != nil {
		return fmt.Errorf("check bootstrap readiness: %w", err)
	}
	if !bootstrapReady {
		log.V(4).Info("Bootstrap data not yet ready, waiting", "machine", client.ObjectKeyFromObject(machine).String())
		original := machine.DeepCopy()
		machine.Status = appprovisioning.StatusWithWaitingForBootstrap(machine)
		if err := r.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
			return fmt.Errorf("set WaitingForBootstrap status: %w", err)
		}
		return nil
	}

	// TODO: PlanはAgentがPlanSecretから取得するため、
	//       Controllerが生成する必要がある。現時点ではプレースホルダーを使用する。
	//       タスク8以降でPlan生成と署名を実装する。
	const placeholderPlanDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

	started, err := r.Provisioner.Start(ctx, machine, placeholderPlanDigest)
	if errors.Is(err, appprovisioning.ErrNoAvailableHost) {
		log.V(4).Info("No available TartHost, will retry", "machine", client.ObjectKeyFromObject(machine).String())
		original := machine.DeepCopy()
		machine.Status = appprovisioning.StatusWithNoAvailableHost(machine)
		if err := r.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
			return fmt.Errorf("set NoAvailableHost status: %w", err)
		}
		// リトライは自然に行われるが、すぐに再試行してもHostが出現しないためrequeue不要
		return nil
	}
	if err != nil {
		return fmt.Errorf("reserve host and start operation: %w", err)
	}
	if err := r.ensureProviderID(ctx, machine, started.Host); err != nil {
		return err
	}

	// HostRef/OperationRefをStatusに永続化する
	original := machine.DeepCopy()
	machine.Status = appprovisioning.StatusWithHostReserved(machine, started.Host, started.Operation)
	if err := r.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("set TartMachine HostRef/OperationRef: %w", err)
	}

	log.Info("TartMachine host reserved and operation started",
		"machine", client.ObjectKeyFromObject(machine).String(),
		"host", client.ObjectKeyFromObject(started.Host).String(),
		"operation", client.ObjectKeyFromObject(started.Operation).String(),
	)
	return nil
}

func (r *TartMachineV1Beta1Reconciler) ensureProviderID(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
) error {
	expected := fmt.Sprintf("tart://%s", host.Name)
	if machine.Spec.ProviderID == expected {
		return nil
	}
	if machine.Spec.ProviderID != "" {
		return fmt.Errorf(
			"TartMachine providerID %q does not match reserved TartHost %q",
			machine.Spec.ProviderID,
			host.Name,
		)
	}
	original := machine.DeepCopy()
	machine.Spec.ProviderID = expected
	if err := r.Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("set TartMachine providerID: %w", err)
	}
	return nil
}

// reconcileOperation はOperationRefが存在する場合にOperationの状態を確認し、
// Succeededの場合はTartMachineをProvisioned済みとしてマークする。
func (r *TartMachineV1Beta1Reconciler) reconcileOperation(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	log := logf.FromContext(ctx)

	operationKey := client.ObjectKey{
		Namespace: machine.Status.OperationRef.Namespace,
		Name:      machine.Status.OperationRef.Name,
	}
	operation := &infrastructurev1beta1.TartHostOperation{}
	if err := r.Get(ctx, operationKey, operation); err != nil {
		if apierrors.IsNotFound(err) {
			// OperationRefが消えた場合はHostRefも無効化する
			log.Info("Referenced TartHostOperation not found, clearing OperationRef",
				"machine", client.ObjectKeyFromObject(machine).String(),
				"operation", operationKey.String(),
			)
			original := machine.DeepCopy()
			machine.Status.OperationRef = nil
			if patchErr := r.Status().Patch(ctx, machine, client.MergeFrom(original)); patchErr != nil {
				return fmt.Errorf("clear stale OperationRef: %w", patchErr)
			}
			return nil
		}
		return fmt.Errorf("get TartHostOperation: %w", err)
	}

	// UID不一致はallocation conflictと同様
	if operation.UID != machine.Status.OperationRef.UID {
		log.Info("TartHostOperation UID mismatch, clearing OperationRef",
			"machine", client.ObjectKeyFromObject(machine).String(),
			"expected", machine.Status.OperationRef.UID,
			"actual", operation.UID,
		)
		original := machine.DeepCopy()
		machine.Status.OperationRef = nil
		if patchErr := r.Status().Patch(ctx, machine, client.MergeFrom(original)); patchErr != nil {
			return fmt.Errorf("clear mismatched OperationRef: %w", patchErr)
		}
		return nil
	}

	command, err := machinelifecycleOperationCommand(false, operation)
	if err != nil {
		return fmt.Errorf("decide TartHostOperation progress: %w", err)
	}

	switch command := command.(type) {
	case machinelifecycledomain.CommandMarkProvisionFailed:
		log.Info("TartHostOperation failed",
			"machine", client.ObjectKeyFromObject(machine).String(),
			"operation", operationKey.String(),
			"phase", operation.Status.Phase,
		)
		original := machine.DeepCopy()
		machine.Status = appprovisioning.StatusWithProvisionFailed(machine,
			command.Reason,
			fmt.Sprintf("TartHostOperation %s/%s %s", operation.Namespace, operation.Name, operation.Status.Phase),
		)
		if patchErr := r.Status().Patch(ctx, machine, client.MergeFrom(original)); patchErr != nil {
			return fmt.Errorf("set provision failed status: %w", patchErr)
		}
	case machinelifecycledomain.CommandObserveProvisionHealth:
		return nil
	default:
		return fmt.Errorf("unexpected TartMachine provisioning command: %T", command)
	}

	return nil
}

// reconcileNodeHealth はNodeHealthを観察してTartMachineのReady状態を更新する。
func (r *TartMachineV1Beta1Reconciler) reconcileNodeHealth(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	if r.NodeHealth == nil {
		return nil
	}

	observation, observed, err := r.NodeHealth.Observe(ctx, machine)
	if err != nil {
		return fmt.Errorf("observe workload Node health: %w", err)
	}
	if !observed {
		return nil
	}

	original := machine.DeepCopy()
	machineState := tartMachineState(machine)
	operation, hasOperation, err := r.referencedOperation(ctx, machine, "health gate")
	if err != nil {
		return err
	}
	if !machineState.Provisioned && hasOperation {
		readiness := appprovisioning.EvaluateReadiness(operation, observation)
		command := machinelifecycledomain.DecideProvisionHealth(machinelifecycledomain.Readiness{
			Ready:   readiness.Ready,
			Reason:  readiness.Reason,
			Message: readiness.Message,
		})
		switch command := command.(type) {
		case machinelifecycledomain.CommandCompleteProvision:
			if r.Provisioner == nil {
				return fmt.Errorf("complete Provisioning: Provisioner is not configured")
			}
			host := &infrastructurev1beta1.TartHost{}
			hostKey := client.ObjectKey{
				Namespace: machine.Status.HostRef.Namespace,
				Name:      machine.Status.HostRef.Name,
			}
			if err := r.Get(ctx, hostKey, host); err != nil {
				return fmt.Errorf("get TartHost for health gate: %w", err)
			}
			if host.UID != machine.Status.HostRef.UID {
				return fmt.Errorf(
					"TartHost UID mismatch for health gate: expected %s, got %s",
					machine.Status.HostRef.UID,
					host.UID,
				)
			}
			if err := r.Provisioner.CompleteProvisioning(ctx, host, operation); err != nil {
				return err
			}
			machine.Status = appprovisioning.StatusWithProvisioned(
				machine,
				machine.Status.Addresses,
				observation.ExpectedVersion,
			)
		case machinelifecycledomain.CommandSetProvisionHealthPending:
			machine.Status = appprovisioning.StatusWithHealthGatePending(
				machine,
				command.Reason,
				command.Message,
			)
		default:
			return fmt.Errorf("unknown Provision health command: %T", command)
		}
	} else if machineState.Provisioned && hasOperation {
		command, err := machinelifecycleOperationCommand(true, operation)
		if err != nil {
			return fmt.Errorf("decide Update health gate: %w", err)
		}
		switch command := command.(type) {
		case machinelifecycledomain.CommandObserveUpdateHealth:
			healthCommand := machinelifecycledomain.DecideUpdateHealth(machinehealthdomain.EvaluateNode(observation))
			switch healthCommand.(type) {
			case machinelifecycledomain.CommandCompleteUpdate:
				if err := r.transitionOperationPhase(
					ctx,
					operation,
					infrastructurev1beta1.TartHostOperationPhaseSucceeded,
				); err != nil {
					return err
				}
				machine.Status = appupdate.StatusWithUpdateSucceeded(machine, operation)
			case machinelifecycledomain.CommandRollbackUpdate:
				if err := r.transitionUpdateFailurePhase(
					ctx,
					operation,
					infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
					infrastructurev1beta1.TartHostOperationPhaseRollingBack,
				); err != nil {
					return err
				}
				machine.Status = applicationhealth.StatusWithNodeHealth(machine, observation)
			default:
				return fmt.Errorf("unknown Update health command: %T", healthCommand)
			}
		case machinelifecycledomain.CommandObserveNodeHealth:
			machine.Status = applicationhealth.StatusWithNodeHealth(machine, observation)
		case machinelifecycledomain.CommandApplyUpdateTerminal:
			if err := r.applyUpdateTerminal(ctx, machine, operation, command.Outcome); err != nil {
				return err
			}
			return nil
		default:
			return fmt.Errorf("unexpected provisioned TartMachine command: %T", command)
		}
	} else {
		// 既存のStatusにNodeHealth Conditionを反映する
		machine.Status = applicationhealth.StatusWithNodeHealth(machine, observation)
	}

	if err := r.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("set TartMachine Node health condition: %w", err)
	}
	return nil
}

func tartMachineState(machine *infrastructurev1beta1.TartMachine) machinelifecycledomain.MachineState {
	return machinelifecycledomain.MachineState{
		Provisioned:  isProvisioned(machine),
		HasOperation: machine.Status.OperationRef != nil,
	}
}

func machinelifecycleOperationCommand(
	provisioned bool,
	operation *infrastructurev1beta1.TartHostOperation,
) (machinelifecycledomain.OperationCommand, error) {
	kind, err := operationdomain.ParseKind(string(operation.Spec.Type))
	if err != nil {
		return nil, err
	}
	var phase operationdomain.Phase
	if operation.Status.Phase != "" {
		phase, err = operationdomain.ParsePhase(string(operation.Status.Phase))
		if err != nil {
			return nil, err
		}
	}
	return machinelifecycledomain.DecideOperation(
		machinelifecycledomain.MachineState{Provisioned: provisioned, HasOperation: true},
		machinelifecycledomain.OperationState{Kind: kind, Phase: phase},
	)
}

func (r *TartMachineV1Beta1Reconciler) referencedOperation(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	purpose string,
) (*infrastructurev1beta1.TartHostOperation, bool, error) {
	if machine.Status.OperationRef == nil {
		return nil, false, nil
	}
	operation := &infrastructurev1beta1.TartHostOperation{}
	key := client.ObjectKey{
		Namespace: machine.Status.OperationRef.Namespace,
		Name:      machine.Status.OperationRef.Name,
	}
	if err := r.Get(ctx, key, operation); err != nil {
		return nil, false, fmt.Errorf("get TartHostOperation for %s: %w", purpose, err)
	}
	if operation.UID != machine.Status.OperationRef.UID {
		return nil, false, fmt.Errorf(
			"TartHostOperation UID mismatch for %s: expected %s, got %s",
			purpose,
			machine.Status.OperationRef.UID,
			operation.UID,
		)
	}
	return operation, true, nil
}

func (r *TartMachineV1Beta1Reconciler) recordUpdateFailureEvent(
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
) {
	if r.Recorder == nil {
		return
	}
	condition := appupdate.FailureCondition(operation)
	if condition == nil {
		return
	}
	r.Recorder.Eventf(
		machine,
		"Warning",
		"UpdateFailed",
		"operationID=%s host=%s operationType=%s failureReason=%s message=%s",
		operation.Spec.OperationID,
		operation.Spec.HostRef.Name,
		operation.Spec.Type,
		condition.Reason,
		condition.Message,
	)
}

func (r *TartMachineV1Beta1Reconciler) transitionOperationPhase(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	target infrastructurev1beta1.TartHostOperationPhase,
) error {
	original := operation.DeepCopy()
	operation.Status.Phase = target
	if operation.Status.ObservedGeneration < operation.Generation {
		operation.Status.ObservedGeneration = operation.Generation
	}
	if err := r.Status().Patch(ctx, operation, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("set TartHostOperation phase: %w", err)
	}
	return nil
}

func (r *TartMachineV1Beta1Reconciler) transitionUpdateFailurePhase(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	failedPhase infrastructurev1beta1.TartHostOperationPhase,
	target infrastructurev1beta1.TartHostOperationPhase,
) error {
	original := operation.DeepCopy()
	operation.Status.Phase = target
	appupdate.UpdateFailureCondition(&operation.Status, operation.Generation, failedPhase, target)
	if operation.Status.ObservedGeneration < operation.Generation {
		operation.Status.ObservedGeneration = operation.Generation
	}
	if err := r.Status().Patch(ctx, operation, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("set TartHostOperation update failure phase: %w", err)
	}
	return nil
}

// isBootstrapReady はCAPIのBootstrap Secretが利用可能かどうかを確認する。
func (r *TartMachineV1Beta1Reconciler) isBootstrapReady(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (bool, error) {
	// util.GetOwnerMachine でCAPIのMachineを取得する
	coreMachine, err := util.GetOwnerMachine(ctx, r.Client, machine.ObjectMeta)
	if err != nil {
		return false, fmt.Errorf("get owner Machine: %w", err)
	}
	if coreMachine == nil {
		return false, nil
	}
	// CAPIのMachineにBootstrapSecretRef.Nameが設定されていて、
	// Bootstrap.DataSecretName（=実際のSecret名）が存在する場合に準備完了とみなす
	if coreMachine.Spec.Bootstrap.DataSecretName == nil {
		return false, nil
	}
	return true, nil
}

func (r *TartMachineV1Beta1Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("tartmachine-v1beta1").
		For(&infrastructurev1beta1.TartMachine{}).
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(util.MachineToInfrastructureMapFunc(
				infrastructurev1beta1.GroupVersion.WithKind("TartMachine"),
			)),
		).
		// TartHostOperationの完了でTartMachineを再reconcileする
		Watches(
			&infrastructurev1beta1.TartHostOperation{},
			handler.EnqueueRequestsFromMapFunc(operationToMachine),
		).
		Complete(r)
}

// operationToMachine はTartHostOperationの変更をMachineRef経由でTartMachineのReconcileに変換する。
func operationToMachine(ctx context.Context, obj client.Object) []ctrl.Request {
	operation, ok := obj.(*infrastructurev1beta1.TartHostOperation)
	if !ok || operation.Spec.MachineRef == nil {
		return nil
	}
	return []ctrl.Request{{
		NamespacedName: client.ObjectKey{
			Namespace: operation.Spec.MachineRef.Namespace,
			Name:      operation.Spec.MachineRef.Name,
		},
	}}
}
