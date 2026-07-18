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
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

func SetupTartHostWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &infrastructurev1beta1.TartHost{}).
		WithValidator(&TartHostCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta1-tarthost,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=create;update,versions=v1beta1,name=vtarthost-v1beta1.kb.io,admissionReviewVersions=v1

type TartHostCustomValidator struct{}

func (v *TartHostCustomValidator) ValidateCreate(_ context.Context, obj *infrastructurev1beta1.TartHost) (admission.Warnings, error) {
	return nil, validateTartHost(obj)
}

func (v *TartHostCustomValidator) ValidateUpdate(_ context.Context, _, newObj *infrastructurev1beta1.TartHost) (admission.Warnings, error) {
	return nil, validateTartHost(newObj)
}

func (v *TartHostCustomValidator) ValidateDelete(_ context.Context, _ *infrastructurev1beta1.TartHost) (admission.Warnings, error) {
	return nil, nil
}

func validateTartHost(host *infrastructurev1beta1.TartHost) error {
	specPath := field.NewPath("spec")
	var errors field.ErrorList
	hints := host.Spec.RootDeviceHints
	if hints.DeviceName == "" && hints.SerialNumber == "" && hints.WWN == "" {
		errors = append(errors, field.Required(specPath.Child("rootDeviceHints"), "at least one stable disk identifier is required"))
	}
	if hints.DeviceName != "" && !strings.HasPrefix(hints.DeviceName, "/dev/disk/by-id/") {
		errors = append(errors, field.Invalid(specPath.Child("rootDeviceHints", "deviceName"), hints.DeviceName, "must use a /dev/disk/by-id path"))
	}
	if host.Spec.ConsumerRef != nil {
		errors = append(errors, validateResourceReference(*host.Spec.ConsumerRef, specPath.Child("consumerRef"))...)
	}
	if len(errors) == 0 {
		return nil
	}
	return apierrors.NewInvalid(schema.GroupKind{Group: infrastructurev1beta1.GroupVersion.Group, Kind: "TartHost"}, host.Name, errors)
}
