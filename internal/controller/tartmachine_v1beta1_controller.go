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
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	appprovisioning "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning"
	appupdate "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
	applicationallocation "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineallocation"
	applicationhealth "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinehealth"
	allocationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/allocation"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

type HostReferenceService interface {
	EnsureMachineHostReference(
		context.Context,
		*infrastructurev1beta1.TartMachine,
	) (allocationdomain.ReferenceResult, error)
}

// ProvisionOrchestrator はv1beta1 TartMachineの初期Provisioningを組み立てる。
type ProvisionOrchestrator interface {
	ReserveAndStartOperation(
		ctx context.Context,
		machine *infrastructurev1beta1.TartMachine,
		planDigest string,
	) (*infrastructurev1beta1.TartHost, *infrastructurev1beta1.TartHostOperation, error)
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
	Provisioner    ProvisionOrchestrator
}

type NodeHealthObserver interface {
	Observe(
		context.Context,
		*infrastructurev1beta1.TartMachine,
	) (machinehealthdomain.NodeObservation, bool, error)
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *TartMachineV1Beta1Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	machine := &infrastructurev1beta1.TartMachine{}
	if err := r.Get(ctx, req.NamespacedName, machine); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get TartMachine: %w", err)
	}

	// すでにProvisioned済みの場合はNodeHealthのみ観察する
	if isProvisioned(machine) {
		if err := r.reconcileUpdateOperation(ctx, machine); err != nil {
			return ctrl.Result{}, err
		}
		return r.reconcileNodeHealth(ctx, machine)
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

	// プロビジョニングフローの実行
	if machine.Status.OperationRef == nil {
		err = r.reconcileProvisionStart(ctx, machine)
	} else {
		err = r.reconcileOperation(ctx, machine)
	}

	if err != nil {
		return ctrl.Result{}, err
	}

	// 常にNodeHealthを観察し、Provisionedへの遷移やProviderIDMismatchの検出を行う
	return r.reconcileNodeHealth(ctx, machine)
}

func (r *TartMachineV1Beta1Reconciler) reconcileUpdateOperation(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	if machine.Status.OperationRef == nil {
		return nil
	}
	operation := &infrastructurev1beta1.TartHostOperation{}
	key := client.ObjectKey{
		Namespace: machine.Status.OperationRef.Namespace,
		Name:      machine.Status.OperationRef.Name,
	}
	if err := r.Get(ctx, key, operation); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get Update TartHostOperation: %w", err)
	}
	if operation.UID != machine.Status.OperationRef.UID ||
		operation.Spec.Type != infrastructurev1beta1.OperationTypeUpdate {
		return nil
	}

	phase, err := operationdomain.ParsePhase(string(operation.Status.Phase))
	if err != nil {
		return fmt.Errorf("parse Update TartHostOperation phase: %w", err)
	}
	if !phase.Terminal() {
		return nil
	}

	original := machine.DeepCopy()
	switch phase {
	case operationdomain.PhaseSucceeded:
		machine.Status = appupdate.StatusWithUpdateSucceeded(machine, operation)
	case operationdomain.PhaseFailed:
		machine.Status = appupdate.StatusWithUpdateRolledBack(machine)
	case operationdomain.PhaseRecoveryRequired:
		machine.Status = appupdate.StatusWithUpdateRecoveryRequired(machine)
	}
	if err := r.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("set TartMachine Update status: %w", err)
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

	host, operation, err := r.Provisioner.ReserveAndStartOperation(ctx, machine, placeholderPlanDigest)
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
	if err := r.ensureProviderID(ctx, machine, host); err != nil {
		return err
	}

	// HostRef/OperationRefをStatusに永続化する
	original := machine.DeepCopy()
	machine.Status = appprovisioning.StatusWithHostReserved(machine, host, operation)
	if err := r.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("set TartMachine HostRef/OperationRef: %w", err)
	}

	log.Info("TartMachine host reserved and operation started",
		"machine", client.ObjectKeyFromObject(machine).String(),
		"host", client.ObjectKeyFromObject(host).String(),
		"operation", client.ObjectKeyFromObject(operation).String(),
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

	if operation.Status.Phase == "" {
		// Phase未設定はまだPendingで開始前
		return nil
	}
	phase, err := operationdomain.ParsePhase(string(operation.Status.Phase))
	if err != nil {
		return fmt.Errorf("parse TartHostOperation phase: %w", err)
	}

	switch phase {
	case operationdomain.PhaseFailed, operationdomain.PhaseRecoveryRequired:
		// OperationがFailed: 失敗としてマークする
		log.Info("TartHostOperation failed",
			"machine", client.ObjectKeyFromObject(machine).String(),
			"operation", operationKey.String(),
			"phase", phase,
		)
		original := machine.DeepCopy()
		machine.Status = appprovisioning.StatusWithProvisionFailed(machine,
			"OperationFailed",
			fmt.Sprintf("TartHostOperation %s/%s %s", operation.Namespace, operation.Name, phase),
		)
		if patchErr := r.Status().Patch(ctx, machine, client.MergeFrom(original)); patchErr != nil {
			return fmt.Errorf("set provision failed status: %w", patchErr)
		}
	}

	// その他のPhaseは進行中または成功（成功の場合は呼び出し元でNodeHealthへ進む）
	return nil
}

// reconcileNodeHealth はNodeHealthを観察してTartMachineのReady状態を更新する。
func (r *TartMachineV1Beta1Reconciler) reconcileNodeHealth(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) (ctrl.Result, error) {
	if r.NodeHealth == nil {
		return ctrl.Result{}, nil
	}

	observation, observed, err := r.NodeHealth.Observe(ctx, machine)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("observe workload Node health: %w", err)
	}
	if !observed {
		return ctrl.Result{}, nil
	}

	original := machine.DeepCopy()
	if !isProvisioned(machine) && machine.Status.OperationRef != nil {
		operation := &infrastructurev1beta1.TartHostOperation{}
		key := client.ObjectKey{
			Namespace: machine.Status.OperationRef.Namespace,
			Name:      machine.Status.OperationRef.Name,
		}
		if err := r.Get(ctx, key, operation); err != nil {
			return ctrl.Result{}, fmt.Errorf("get TartHostOperation for health gate: %w", err)
		}
		if operation.UID != machine.Status.OperationRef.UID {
			return ctrl.Result{}, fmt.Errorf(
				"TartHostOperation UID mismatch for health gate: expected %s, got %s",
				machine.Status.OperationRef.UID,
				operation.UID,
			)
		}
		readiness := appprovisioning.EvaluateReadiness(operation, observation)
		if readiness.Ready {
			if r.Provisioner == nil {
				return ctrl.Result{}, fmt.Errorf("complete Provisioning: Provisioner is not configured")
			}
			host := &infrastructurev1beta1.TartHost{}
			hostKey := client.ObjectKey{
				Namespace: machine.Status.HostRef.Namespace,
				Name:      machine.Status.HostRef.Name,
			}
			if err := r.Get(ctx, hostKey, host); err != nil {
				return ctrl.Result{}, fmt.Errorf("get TartHost for health gate: %w", err)
			}
			if host.UID != machine.Status.HostRef.UID {
				return ctrl.Result{}, fmt.Errorf(
					"TartHost UID mismatch for health gate: expected %s, got %s",
					machine.Status.HostRef.UID,
					host.UID,
				)
			}
			if err := r.Provisioner.CompleteProvisioning(ctx, host, operation); err != nil {
				return ctrl.Result{}, err
			}
			machine.Status = appprovisioning.StatusWithProvisioned(machine, machine.Status.Addresses)
		} else {
			machine.Status = appprovisioning.StatusWithHealthGatePending(
				machine,
				readiness.Reason,
				readiness.Message,
			)
		}
	} else if isProvisioned(machine) && machine.Status.OperationRef != nil {
		operation := &infrastructurev1beta1.TartHostOperation{}
		key := client.ObjectKey{
			Namespace: machine.Status.OperationRef.Namespace,
			Name:      machine.Status.OperationRef.Name,
		}
		if err := r.Get(ctx, key, operation); err != nil {
			return ctrl.Result{}, fmt.Errorf("get Update TartHostOperation for health gate: %w", err)
		}
		if operation.UID != machine.Status.OperationRef.UID {
			return ctrl.Result{}, fmt.Errorf(
				"TartHostOperation UID mismatch for update health gate: expected %s, got %s",
				machine.Status.OperationRef.UID,
				operation.UID,
			)
		}
		if operation.Spec.Type == infrastructurev1beta1.OperationTypeUpdate &&
			operation.Status.Phase == infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth {
			health := machinehealthdomain.EvaluateNode(observation)
			if health.Ready {
				if err := r.transitionOperationPhase(
					ctx,
					operation,
					infrastructurev1beta1.TartHostOperationPhaseSucceeded,
				); err != nil {
					return ctrl.Result{}, err
				}
				machine.Status = appupdate.StatusWithUpdateSucceeded(machine, operation)
			} else {
				machine.Status = applicationhealth.StatusWithNodeHealth(machine, observation)
			}
		} else {
			machine.Status = applicationhealth.StatusWithNodeHealth(machine, observation)
		}
	} else {
		// 既存のStatusにNodeHealth Conditionを反映する
		machine.Status = applicationhealth.StatusWithNodeHealth(machine, observation)
	}

	if err := r.Status().Patch(ctx, machine, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, fmt.Errorf("set TartMachine Node health condition: %w", err)
	}
	return ctrl.Result{}, nil
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
