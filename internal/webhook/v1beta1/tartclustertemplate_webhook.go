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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

func SetupTartClusterTemplateWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &infrastructurev1beta1.TartClusterTemplate{}).
		WithValidator(&TartClusterTemplateCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta1-tartclustertemplate,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=tartclustertemplates,verbs=create;update,versions=v1beta1,name=vtartclustertemplate-v1beta1.kb.io,admissionReviewVersions=v1

type TartClusterTemplateCustomValidator struct{}

func (v *TartClusterTemplateCustomValidator) ValidateCreate(_ context.Context, obj *infrastructurev1beta1.TartClusterTemplate) (admission.Warnings, error) {
	return nil, validateTartClusterTemplate(obj)
}

func (v *TartClusterTemplateCustomValidator) ValidateUpdate(_ context.Context, _, newObj *infrastructurev1beta1.TartClusterTemplate) (admission.Warnings, error) {
	return nil, validateTartClusterTemplate(newObj)
}

func (v *TartClusterTemplateCustomValidator) ValidateDelete(_ context.Context, _ *infrastructurev1beta1.TartClusterTemplate) (admission.Warnings, error) {
	return nil, nil
}

func validateTartClusterTemplate(template *infrastructurev1beta1.TartClusterTemplate) error {
	errors := validateArtifactPolicy(template.Spec.Template.Spec.ArtifactPolicy, field.NewPath("spec", "template", "spec", "artifactPolicy"))
	if len(errors) == 0 {
		return nil
	}
	return apierrors.NewInvalid(schema.GroupKind{Group: infrastructurev1beta1.GroupVersion.Group, Kind: "TartClusterTemplate"}, template.Name, errors)
}
