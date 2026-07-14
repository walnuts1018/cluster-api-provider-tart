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

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinetemplatelifecycle "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinetemplatelifecycle"
	resourcefinalizer "github.com/walnuts1018/cluster-api-provider-tart/internal/application/resourcefinalizer"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/telemetry"
)

// TartMachineTemplateReconciler reconciles a TartMachineTemplate object
type TartMachineTemplateReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Lifecycle *machinetemplatelifecycle.Workflow
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachinetemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachinetemplates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachinetemplates/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the template closer to the desired state.
func (r *TartMachineTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, span := telemetry.Tracer.Start(ctx, "TartMachineTemplate.Reconcile")
	span.SetAttributes(
		attribute.String("kubernetes.resource.name", req.Name),
		attribute.String("kubernetes.resource.namespace", req.Namespace),
	)
	defer span.End()

	var template infrastructurev1beta1.TartMachineTemplate
	if err := r.Get(ctx, req.NamespacedName, &template); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if _, err := r.lifecycleWorkflow().Reconcile(ctx, &template); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *TartMachineTemplateReconciler) lifecycleWorkflow() *machinetemplatelifecycle.Workflow {
	if r.Lifecycle != nil {
		return r.Lifecycle
	}
	return machinetemplatelifecycle.NewWorkflowWithFinalizer(resourcefinalizer.NewTartMachineTemplateWorkflow(r.Client))
}

// SetupWithManager sets up the controller with the Manager.
func (r *TartMachineTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1beta1.TartMachineTemplate{}).
		Named("tartmachinetemplate").
		Complete(r)
}
