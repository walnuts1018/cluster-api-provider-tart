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
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	operationexecution "github.com/walnuts1018/cluster-api-provider-tart/internal/application/operationexecution"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/telemetry"
)

// TartHostOperationReconciler は TartHostOperation の Phase を進める。
// Phase の詳細なビジネスロジック（書き込みや検証）は Agent が行うため、
// このControllerはPreparingBootへの遷移とAwaitingHealth判定に集中する。
type TartHostOperationReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	Workflow           *operationexecution.Workflow
	PowerOn            operationexecution.PowerOnService
	PrepareBoot        operationexecution.BootPreparationService
	HostPhase          operationexecution.HostPhaseService
	Targets            operationexecution.DriverTargetBuilder
	DriverCapabilities operationexecution.DriverCapabilityObserver
	DriverPowerState   operationexecution.DriverPowerStateObserver
	DriverBootState    operationexecution.DriverBootStateObserver
	Recorder           record.EventRecorder
}

const (
	operationDeadlineRequeueInterval = operationexecution.DeadlineRequeueInterval
	redfishDriverName                = "redfish"
)

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthostoperations,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthostoperations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts/status,verbs=get;update;patch

func (r *TartHostOperationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
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

	result, err := r.operationWorkflow().Reconcile(ctx, &operation)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := r.applyResult(ctx, &operation, result); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: result.RequeueAfter}, nil
}

func (r *TartHostOperationReconciler) applyResult(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	result operationexecution.Result,
) error {
	if result.StatusCondition != nil {
		if err := r.patchResultCondition(ctx, operation, *result.StatusCondition); err != nil {
			return err
		}
	}
	if result.Event != nil && r.Recorder != nil {
		r.Recorder.Event(operation, result.Event.Type, result.Event.Reason, result.Event.Message)
	}
	return nil
}

func (r *TartHostOperationReconciler) patchResultCondition(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	condition metav1.Condition,
) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &infrastructurev1beta1.TartHostOperation{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(operation), current); err != nil {
			return fmt.Errorf("get TartHostOperation for result condition: %w", err)
		}
		original := current.DeepCopy()
		condition.ObservedGeneration = current.Generation
		apimeta.SetStatusCondition(&current.Status.Conditions, condition)
		if current.Status.ObservedGeneration < current.Generation {
			current.Status.ObservedGeneration = current.Generation
		}
		return r.Status().Patch(ctx, current, client.MergeFrom(original))
	})
}

func (r *TartHostOperationReconciler) operationWorkflow() *operationexecution.Workflow {
	if r.Workflow != nil {
		return r.Workflow
	}
	r.Workflow = operationexecution.NewWorkflow(
		r.Client,
		r.PowerOn,
		r.PrepareBoot,
		r.HostPhase,
		r.Targets,
		r.DriverCapabilities,
		r.DriverPowerState,
		r.DriverBootState,
	)
	return r.Workflow
}

func (r *TartHostOperationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("tarthostoperation")
	}
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
