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

func SetupTartMachineWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &infrastructurev1beta1.TartMachine{}).
		WithValidator(&TartMachineCustomValidator{}).
		WithDefaulter(&TartMachineCustomDefaulter{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-infrastructure-cluster-x-k8s-io-v1beta1-tartmachine,mutating=true,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=tartmachines,verbs=create;update,versions=v1beta1,name=mtartmachine-v1beta1.kb.io,admissionReviewVersions=v1

type TartMachineCustomDefaulter struct{}

func (d *TartMachineCustomDefaulter) Default(_ context.Context, obj *infrastructurev1beta1.TartMachine) error {
	if obj.Spec.UpdatePolicy.Mode == "" {
		obj.Spec.UpdatePolicy.Mode = infrastructurev1beta1.UpdateModeReplace
	}
	return nil
}

// +kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta1-tartmachine,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=tartmachines;tartmachines/status,verbs=create;update,versions=v1beta1,name=vtartmachine-v1beta1.kb.io,admissionReviewVersions=v1

type TartMachineCustomValidator struct{}

func (v *TartMachineCustomValidator) ValidateCreate(_ context.Context, obj *infrastructurev1beta1.TartMachine) (admission.Warnings, error) {
	return nil, validateTartMachine(obj, nil)
}

func (v *TartMachineCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj *infrastructurev1beta1.TartMachine) (admission.Warnings, error) {
	return nil, validateTartMachine(newObj, oldObj)
}

func (v *TartMachineCustomValidator) ValidateDelete(_ context.Context, _ *infrastructurev1beta1.TartMachine) (admission.Warnings, error) {
	return nil, nil
}

func validateTartMachine(machine, oldMachine *infrastructurev1beta1.TartMachine) error {
	errors := validateImage(machine.Spec.Image, field.NewPath("spec", "image"))
	if oldMachine != nil &&
		oldMachine.Status.Initialization.Provisioned != nil &&
		*oldMachine.Status.Initialization.Provisioned &&
		machine.Status.Initialization.Provisioned != nil &&
		!*machine.Status.Initialization.Provisioned {
		errors = append(errors, field.Forbidden(field.NewPath("status", "initialization", "provisioned"), "must not transition from true to false"))
	}
	if len(errors) == 0 {
		return nil
	}
	return apierrors.NewInvalid(schema.GroupKind{Group: infrastructurev1beta1.GroupVersion.Group, Kind: "TartMachine"}, machine.Name, errors)
}
