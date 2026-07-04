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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthostoperations,verbs=get;list;watch

func SetupTartHostOperationWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &infrastructurev1beta1.TartHostOperation{}).
		WithValidator(&TartHostOperationCustomValidator{client: mgr.GetClient()}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta1-tarthostoperation,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=tarthostoperations,verbs=create;update,versions=v1beta1,name=vtarthostoperation-v1beta1.kb.io,admissionReviewVersions=v1

type TartHostOperationCustomValidator struct {
	client client.Client
}

func (v *TartHostOperationCustomValidator) ValidateCreate(ctx context.Context, obj *infrastructurev1beta1.TartHostOperation) (admission.Warnings, error) {
	errors := validateOperationSpec(obj.Spec)
	if len(errors) == 0 {
		errors = append(errors, v.validateSingleActiveOperation(ctx, obj)...)
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

func (v *TartHostOperationCustomValidator) validateSingleActiveOperation(ctx context.Context, operation *infrastructurev1beta1.TartHostOperation) field.ErrorList {
	if v.client == nil {
		return nil
	}

	var operations infrastructurev1beta1.TartHostOperationList
	if err := v.client.List(ctx, &operations, client.InNamespace(operation.Namespace)); err != nil {
		return field.ErrorList{field.InternalError(field.NewPath("spec", "hostRef"), err)}
	}
	for index := range operations.Items {
		existing := &operations.Items[index]
		if existing.Name == operation.Name ||
			existing.Spec.HostRef.UID != operation.Spec.HostRef.UID ||
			operationPhaseTerminal(existing.Status.Phase) {
			continue
		}
		return field.ErrorList{
			field.Forbidden(field.NewPath("spec", "hostRef"), "another non-terminal operation already exists for this host"),
		}
	}
	return nil
}

func operationPhaseTerminal(phase infrastructurev1beta1.TartHostOperationPhase) bool {
	switch phase {
	case infrastructurev1beta1.TartHostOperationPhaseSucceeded,
		infrastructurev1beta1.TartHostOperationPhaseFailed,
		infrastructurev1beta1.TartHostOperationPhaseRecoveryRequired:
		return true
	case infrastructurev1beta1.TartHostOperationPhasePending,
		infrastructurev1beta1.TartHostOperationPhasePreparingBoot,
		infrastructurev1beta1.TartHostOperationPhaseWaitingForAgent,
		infrastructurev1beta1.TartHostOperationPhaseWriting,
		infrastructurev1beta1.TartHostOperationPhaseVerifying,
		infrastructurev1beta1.TartHostOperationPhaseBootTrial,
		infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
		infrastructurev1beta1.TartHostOperationPhaseDistributionUpdating,
		infrastructurev1beta1.TartHostOperationPhaseRollingBack,
		"":
		return false
	}
	return false
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
