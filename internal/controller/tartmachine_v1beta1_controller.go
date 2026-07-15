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
	"k8s.io/client-go/tools/events"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinedeletion "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinedeletion"
	machineexecution "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution"
	machinelifecycle "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinelifecycle"
	resourcefinalizer "github.com/walnuts1018/cluster-api-provider-tart/internal/application/resourcefinalizer"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/telemetry"
)

type TartMachineV1Beta1Reconciler struct {
	client.Client
	Lifecycle      *machinelifecycle.Workflow
	Execution      machinelifecycle.ExecutionStep
	Deletion       machinelifecycle.DeletionStep
	Finalizer      machinelifecycle.FinalizerStep
	HostReferences machineexecution.HostReferenceService
	NodeHealth     machineexecution.NodeHealthObserver
	Provisioner    machineexecution.ProvisionStep
	Cleaner        machinedeletion.CleaningStep
	Recorder       events.EventRecorder
}

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
	_, err := r.lifecycleWorkflow().Reconcile(ctx, machine)
	return ctrl.Result{}, err
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

func (r *TartMachineV1Beta1Reconciler) lifecycleWorkflow() *machinelifecycle.Workflow {
	if r.Lifecycle != nil {
		return r.Lifecycle
	}
	return machinelifecycle.NewWorkflowWithSteps(
		r.finalizerStep(),
		r.executionStep(),
		r.deletionStep(),
	)
}

func (r *TartMachineV1Beta1Reconciler) executionStep() machinelifecycle.ExecutionStep {
	if r.Execution != nil {
		return r.Execution
	}
	return machineexecution.NewWorkflow(r.Client, r.HostReferences, r.NodeHealth, r.Provisioner, r.Recorder)
}

func (r *TartMachineV1Beta1Reconciler) deletionStep() machinelifecycle.DeletionStep {
	if r.Deletion != nil {
		return r.Deletion
	}
	return machinedeletion.NewWorkflow(r.Client, r.Cleaner)
}

func (r *TartMachineV1Beta1Reconciler) finalizerStep() machinelifecycle.FinalizerStep {
	if r.Finalizer != nil {
		return r.Finalizer
	}
	return resourcefinalizer.NewTartMachineWorkflow(r.Client)
}
