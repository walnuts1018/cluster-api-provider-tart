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

package v1beta1

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

func SetupTartMachineTemplateWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &infrastructurev1beta1.TartMachineTemplate{}).
		WithValidator(&TartMachineTemplateCustomValidator{}).
		WithDefaulter(&TartMachineTemplateCustomDefaulter{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-infrastructure-cluster-x-k8s-io-v1beta1-tartmachinetemplate,mutating=true,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=tartmachinetemplates,verbs=create;update,versions=v1beta1,name=mtartmachinetemplate-v1beta1.kb.io,admissionReviewVersions=v1

type TartMachineTemplateCustomDefaulter struct{}

func (d *TartMachineTemplateCustomDefaulter) Default(_ context.Context, obj *infrastructurev1beta1.TartMachineTemplate) error {
	if obj.Spec.Template.Spec.UpdatePolicy.Mode == "" {
		obj.Spec.Template.Spec.UpdatePolicy.Mode = infrastructurev1beta1.UpdateModeReplace
	}
	return nil
}

// +kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta1-tartmachinetemplate,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=tartmachinetemplates,verbs=create;update,versions=v1beta1,name=vtartmachinetemplate-v1beta1.kb.io,admissionReviewVersions=v1

type TartMachineTemplateCustomValidator struct{}

func (v *TartMachineTemplateCustomValidator) ValidateCreate(_ context.Context, obj *infrastructurev1beta1.TartMachineTemplate) (admission.Warnings, error) {
	return nil, validateTartMachineTemplate(obj)
}

func (v *TartMachineTemplateCustomValidator) ValidateUpdate(_ context.Context, _, newObj *infrastructurev1beta1.TartMachineTemplate) (admission.Warnings, error) {
	return nil, validateTartMachineTemplate(newObj)
}

func (v *TartMachineTemplateCustomValidator) ValidateDelete(_ context.Context, _ *infrastructurev1beta1.TartMachineTemplate) (admission.Warnings, error) {
	return nil, nil
}

func validateTartMachineTemplate(template *infrastructurev1beta1.TartMachineTemplate) error {
	errors := validateImage(template.Spec.Template.Spec.Image, field.NewPath("spec", "template", "spec", "image"))
	if len(errors) == 0 {
		return nil
	}
	return apierrors.NewInvalid(schema.GroupKind{Group: infrastructurev1beta1.GroupVersion.Group, Kind: "TartMachineTemplate"}, template.Name, errors)
}
