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

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/telemetry"
)

// TartMachineTemplateReconciler reconciles a TartMachineTemplate object
type TartMachineTemplateReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const tartMachineTemplateFinalizer = "infrastructure.cluster.x-k8s.io/tartmachinetemplate"

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachinetemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachinetemplates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachinetemplates/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the template closer to the desired state.
func (r *TartMachineTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	ctx, span := telemetry.Tracer.Start(ctx, "TartMachineTemplate.Reconcile")
	span.SetAttributes(
		attribute.String("kubernetes.resource.name", req.Name),
		attribute.String("kubernetes.resource.namespace", req.Namespace),
	)
	defer span.End()

	var template infrastructurev1alpha1.TartMachineTemplate
	if err := r.Get(ctx, req.NamespacedName, &template); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	log.V(4).Info("Reconciling TartMachineTemplate")

	if !template.DeletionTimestamp.IsZero() {
		if err := r.reconcileDelete(ctx, &template); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if err := r.ensureFinalizer(ctx, &template); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *TartMachineTemplateReconciler) ensureFinalizer(ctx context.Context, template *infrastructurev1alpha1.TartMachineTemplate) error {
	if controllerutil.ContainsFinalizer(template, tartMachineTemplateFinalizer) {
		return nil
	}

	original := template.DeepCopy()
	controllerutil.AddFinalizer(template, tartMachineTemplateFinalizer)
	return r.Patch(ctx, template, client.MergeFrom(original))
}

func (r *TartMachineTemplateReconciler) reconcileDelete(ctx context.Context, template *infrastructurev1alpha1.TartMachineTemplate) error {
	if !controllerutil.ContainsFinalizer(template, tartMachineTemplateFinalizer) {
		return nil
	}

	original := template.DeepCopy()
	controllerutil.RemoveFinalizer(template, tartMachineTemplateFinalizer)
	return r.Patch(ctx, template, client.MergeFrom(original))
}

// SetupWithManager sets up the controller with the Manager.
func (r *TartMachineTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.TartMachineTemplate{}).
		Named("tartmachinetemplate").
		Complete(r)
}
