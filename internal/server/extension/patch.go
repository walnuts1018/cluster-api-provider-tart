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

package extension

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	application "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/inplaceupdate"
)

func decodeTartMachine(raw runtime.RawExtension) (infrastructurev1beta1.TartMachine, error) {
	var machine infrastructurev1beta1.TartMachine
	if err := json.Unmarshal(raw.Raw, &machine); err != nil {
		return infrastructurev1beta1.TartMachine{}, fmt.Errorf("decode TartMachine: %w", err)
	}
	return machine, nil
}

func decodeTartMachineTemplate(raw runtime.RawExtension) (infrastructurev1beta1.TartMachineTemplate, error) {
	var template infrastructurev1beta1.TartMachineTemplate
	if err := json.Unmarshal(raw.Raw, &template); err != nil {
		return infrastructurev1beta1.TartMachineTemplate{}, fmt.Errorf("decode TartMachineTemplate: %w", err)
	}
	return template, nil
}

func classifyMachineRequest(
	request *runtimehooksv1.CanUpdateMachineRequest,
) (domain.Classification, infrastructurev1beta1.TartMachine, error) {
	current, err := decodeTartMachine(request.Current.InfrastructureMachine)
	if err != nil {
		return domain.Classification{}, infrastructurev1beta1.TartMachine{}, err
	}
	desired, err := decodeTartMachine(request.Desired.InfrastructureMachine)
	if err != nil {
		return domain.Classification{}, infrastructurev1beta1.TartMachine{}, err
	}
	decision, err := application.ClassifyChanges(application.ClassificationInput{
		CurrentMachine:         request.Current.Machine,
		DesiredMachine:         request.Desired.Machine,
		CurrentTartMachine:     current,
		DesiredTartMachine:     desired,
		CurrentBootstrapConfig: request.Current.BootstrapConfig,
		DesiredBootstrapConfig: request.Desired.BootstrapConfig,
	})
	if err != nil {
		return domain.Classification{}, infrastructurev1beta1.TartMachine{}, err
	}
	return classificationFromDecision(decision), desired, nil
}

func classifyMachineSetRequest(
	request *runtimehooksv1.CanUpdateMachineSetRequest,
) (domain.Classification, infrastructurev1beta1.TartMachineTemplateResourceSpec, error) {
	current, err := decodeTartMachineTemplate(request.Current.InfrastructureMachineTemplate)
	if err != nil {
		return domain.Classification{}, infrastructurev1beta1.TartMachineTemplateResourceSpec{}, err
	}
	desired, err := decodeTartMachineTemplate(request.Desired.InfrastructureMachineTemplate)
	if err != nil {
		return domain.Classification{}, infrastructurev1beta1.TartMachineTemplateResourceSpec{}, err
	}
	currentBootstrap, err := templateSpec(request.Current.BootstrapConfigTemplate)
	if err != nil {
		return domain.Classification{}, infrastructurev1beta1.TartMachineTemplateResourceSpec{}, err
	}
	desiredBootstrap, err := templateSpec(request.Desired.BootstrapConfigTemplate)
	if err != nil {
		return domain.Classification{}, infrastructurev1beta1.TartMachineTemplateResourceSpec{}, err
	}
	currentMachine := clusterv1.Machine{Spec: request.Current.MachineSet.Spec.Template.Spec}
	desiredMachine := clusterv1.Machine{Spec: request.Desired.MachineSet.Spec.Template.Spec}
	decision, err := application.ClassifyChanges(application.ClassificationInput{
		CurrentMachine:         currentMachine,
		DesiredMachine:         desiredMachine,
		CurrentTartMachine:     tartMachineFromTemplate(current.Spec.Template.Spec),
		DesiredTartMachine:     tartMachineFromTemplate(desired.Spec.Template.Spec),
		CurrentBootstrapConfig: currentBootstrap,
		DesiredBootstrapConfig: desiredBootstrap,
	})
	if err != nil {
		return domain.Classification{}, infrastructurev1beta1.TartMachineTemplateResourceSpec{}, err
	}
	return classificationFromDecision(decision), desired.Spec.Template.Spec, nil
}

func tartMachineFromTemplate(spec infrastructurev1beta1.TartMachineTemplateResourceSpec) infrastructurev1beta1.TartMachine {
	return infrastructurev1beta1.TartMachine{
		Spec: infrastructurev1beta1.TartMachineSpec{
			Image:           spec.Image,
			PlatformProfile: spec.PlatformProfile,
			HostSelector:    spec.HostSelector,
			UpdatePolicy:    spec.UpdatePolicy,
			DeletionPolicy:  spec.DeletionPolicy,
		},
	}
}

func templateSpec(raw runtime.RawExtension) (runtime.RawExtension, error) {
	if len(raw.Raw) == 0 && raw.Object == nil {
		return runtime.RawExtension{}, nil
	}
	var object struct {
		Spec struct {
			Template struct {
				Spec json.RawMessage `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(raw.Raw, &object); err != nil {
		return runtime.RawExtension{}, fmt.Errorf("decode BootstrapConfigTemplate: %w", err)
	}
	if len(object.Spec.Template.Spec) == 0 {
		return runtime.RawExtension{}, fmt.Errorf("decode BootstrapConfigTemplate: spec.template.spec is required")
	}
	wrapped, err := json.Marshal(map[string]json.RawMessage{"spec": object.Spec.Template.Spec})
	if err != nil {
		return runtime.RawExtension{}, fmt.Errorf("encode BootstrapConfig spec: %w", err)
	}
	return runtime.RawExtension{Raw: wrapped}, nil
}

func machinePatch(
	classification domain.Classification,
	desired infrastructurev1beta1.TartMachineSpec,
) (runtimehooksv1.Patch, error) {
	spec := osOnlySpecPatch(classification, desired.Image, desired.UpdatePolicy)
	return jsonMergePatch(map[string]any{"spec": spec})
}

func machineTemplatePatch(
	classification domain.Classification,
	desired infrastructurev1beta1.TartMachineTemplateResourceSpec,
) (runtimehooksv1.Patch, error) {
	spec := osOnlySpecPatch(classification, desired.Image, desired.UpdatePolicy)
	return jsonMergePatch(map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"spec": spec,
			},
		},
	})
}

func osOnlySpecPatch(
	classification domain.Classification,
	image infrastructurev1beta1.ImageSpec,
	updatePolicy infrastructurev1beta1.UpdatePolicy,
) map[string]any {
	spec := map[string]any{}
	for _, path := range classification.Allowed {
		//nolint:exhaustive // This patch only emits fields currently allowed for OS-only in-place updates.
		switch path {
		case domain.FieldTartMachineImageRef:
			spec["image"] = map[string]any{"ref": image.Ref}
		case domain.FieldTartMachineUpdatePolicy:
			spec["updatePolicy"] = map[string]any{"mode": updatePolicy.Mode}
		}
	}
	return spec
}

func classificationFromDecision(decision domain.EligibilityDecision) domain.Classification {
	switch decided := decision.(type) {
	case domain.NoEligibleChange:
		return decided.Classification
	case domain.EligibleForInPlaceUpdate:
		return decided.Classification
	case domain.IneligibleForInPlaceUpdate:
		return decided.Classification
	default:
		return domain.Classification{}
	}
}

func jsonMergePatch(value map[string]any) (runtimehooksv1.Patch, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return runtimehooksv1.Patch{}, fmt.Errorf("encode JSON Merge Patch: %w", err)
	}
	return runtimehooksv1.Patch{
		PatchType: runtimehooksv1.JSONMergePatchType,
		Patch:     data,
	}, nil
}
