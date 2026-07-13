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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machineexecution "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/telemetry"
)

type TartMachineV1Beta1Reconciler struct {
	client.Client
	MachineWorkflow *machineexecution.Workflow
	HostReferences  machineexecution.HostReferenceService
	NodeHealth      machineexecution.NodeHealthObserver
	Provisioner     machineexecution.ProvisionWorkflow
	Cleaner         CleaningWorkflow
	Recorder        record.EventRecorder
}

type CleaningWorkflow interface {
	StartCleaning(
		ctx context.Context,
		machine *infrastructurev1beta1.TartMachine,
		host *infrastructurev1beta1.TartHost,
	) (*infrastructurev1beta1.TartHostOperation, error)
}

const tartMachineCleanupFinalizer = "infrastructure.cluster.x-k8s.io/tartmachine-cleanup"

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines/finalizers,verbs=update
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthostoperations,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *TartMachineV1Beta1Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
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

	if err := r.machineWorkflow().Reconcile(ctx, machine); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *TartMachineV1Beta1Reconciler) machineWorkflow() *machineexecution.Workflow {
	if r.MachineWorkflow != nil {
		return r.MachineWorkflow
	}
	return machineexecution.NewWorkflow(r.Client, r.HostReferences, r.NodeHealth, r.Provisioner, r.Recorder)
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
