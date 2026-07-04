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

package v1beta1

import (
	"context"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

func SetupTartHostOperationWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &infrastructurev1beta1.TartHostOperation{}).
		WithValidator(&TartHostOperationCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta1-tarthostoperation,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=tarthostoperations,verbs=create;update,versions=v1beta1,name=vtarthostoperation-v1beta1.kb.io,admissionReviewVersions=v1

type TartHostOperationCustomValidator struct{}

func (v *TartHostOperationCustomValidator) ValidateCreate(_ context.Context, obj *infrastructurev1beta1.TartHostOperation) (admission.Warnings, error) {
	errors := validateOperationSpec(obj.Spec)
	expectedName, err := operationdomain.ResourceName(string(obj.Spec.HostRef.UID))
	if err == nil && obj.Name != expectedName {
		errors = append(errors, field.Invalid(
			field.NewPath("metadata", "name"),
			obj.Name,
			"must be the deterministic active operation name for spec.hostRef.uid",
		))
	}
	return nil, invalidOperation(obj, errors)
}

func (v *TartHostOperationCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj *infrastructurev1beta1.TartHostOperation) (admission.Warnings, error) {
	errors := validateOperationSpec(newObj.Spec)
	if !apiequality.Semantic.DeepEqual(oldObj.Spec, newObj.Spec) {
		errors = append(errors, field.Forbidden(field.NewPath("spec"), "operation spec is immutable"))
	}
	return nil, invalidOperation(newObj, errors)
}

func (v *TartHostOperationCustomValidator) ValidateDelete(_ context.Context, _ *infrastructurev1beta1.TartHostOperation) (admission.Warnings, error) {
	return nil, nil
}

func invalidOperation(operation *infrastructurev1beta1.TartHostOperation, errors field.ErrorList) error {
	if len(errors) == 0 {
		return nil
	}
	return apierrors.NewInvalid(
		schema.GroupKind{Group: infrastructurev1beta1.GroupVersion.Group, Kind: "TartHostOperation"},
		operation.Name,
		errors,
	)
}
