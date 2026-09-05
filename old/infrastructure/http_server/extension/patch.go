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
	"slices"

	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	domain "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/entity/inplaceupdate"
	application "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/update_machine"
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

func bootstrapSpecSnapshot(extension runtime.RawExtension) (any, error) {
	if len(extension.Raw) == 0 && extension.Object == nil {
		return map[string]any{}, nil
	}
	raw := extension.Raw
	if len(raw) == 0 {
		var err error
		raw, err = json.Marshal(extension.Object)
		if err != nil {
			return nil, fmt.Errorf("marshal object: %w", err)
		}
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("unmarshal JSON: %w", err)
	}
	return object["spec"], nil
}

func machinePatch(
	classification domain.Classification,
	desired infrastructurev1beta1.TartMachineSpec,
) (runtimehooksv1.Patch, error) {
	spec := osOnlySpecPatch(classification, desired.Image, desired.UpdatePolicy)
	return jsonMergePatch(map[string]any{"spec": spec})
}

func machineBootstrapPatch(
	classification domain.Classification,
	desired runtime.RawExtension,
) (runtimehooksv1.Patch, error) {
	if !classificationAllows(classification, domain.FieldBootstrapConfig) {
		return runtimehooksv1.Patch{}, nil
	}
	spec, err := bootstrapSpecSnapshot(desired)
	if err != nil {
		return runtimehooksv1.Patch{}, fmt.Errorf("decode desired BootstrapConfig: %w", err)
	}
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

func machineSetBootstrapTemplatePatch(
	classification domain.Classification,
	desired runtime.RawExtension,
) (runtimehooksv1.Patch, error) {
	if !classificationAllows(classification, domain.FieldBootstrapConfig) {
		return runtimehooksv1.Patch{}, nil
	}
	spec, err := templateSpec(desired)
	if err != nil {
		return runtimehooksv1.Patch{}, err
	}
	bootstrapSpec, err := bootstrapSpecSnapshot(spec)
	if err != nil {
		return runtimehooksv1.Patch{}, fmt.Errorf("decode desired BootstrapConfigTemplate spec: %w", err)
	}
	return jsonMergePatch(map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"spec": bootstrapSpec,
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
		switch path {
		case domain.FieldTartMachineImageRef:
			spec["image"] = map[string]any{"ref": image.Ref}
		case domain.FieldTartMachineUpdatePolicy:
			spec["updatePolicy"] = map[string]any{"mode": updatePolicy.Mode}
		case domain.FieldMachineVersion,
			domain.FieldMachineSpec,
			domain.FieldBootstrapConfig,
			domain.FieldTartMachinePlatformProfile,
			domain.FieldTartMachineHostSelector,
			domain.FieldTartMachineProviderID,
			domain.FieldTartMachineDeletionPolicy:
			// No action needed for these fields in OS-only spec patch.
		}
	}
	return spec
}

func classificationAllows(classification domain.Classification, path domain.FieldPath) bool {
	return slices.Contains(classification.Allowed, path)
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
