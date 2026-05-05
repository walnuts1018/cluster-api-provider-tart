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

package extension

import (
	"context"
	"encoding/json"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// HandleCanUpdateMachineSet handles the CanUpdateMachineSet hook.
// It compares the current and desired TartMachineTemplate specs and determines
// whether the change can be handled in-place or requires a rolling update.
func HandleCanUpdateMachineSet(ctx context.Context, req *runtimehooksv1.CanUpdateMachineSetRequest, resp *runtimehooksv1.CanUpdateMachineSetResponse) {
	log := ctrllog.FromContext(ctx)

	// Unmarshal the current and desired infrastructure machine templates.
	var currentTemplate infrastructurev1alpha1.TartMachineTemplate
	if err := json.Unmarshal(req.Current.InfrastructureMachineTemplate.Raw, &currentTemplate); err != nil {
		log.Error(err, "Failed to unmarshal current infrastructure machine template")
		resp.SetStatus(runtimehooksv1.ResponseStatusFailure)
		resp.SetMessage("failed to unmarshal current infrastructure machine template: " + err.Error())
		return
	}

	var desiredTemplate infrastructurev1alpha1.TartMachineTemplate
	if err := json.Unmarshal(req.Desired.InfrastructureMachineTemplate.Raw, &desiredTemplate); err != nil {
		log.Error(err, "Failed to unmarshal desired infrastructure machine template")
		resp.SetStatus(runtimehooksv1.ResponseStatusFailure)
		resp.SetMessage("failed to unmarshal desired infrastructure machine template: " + err.Error())
		return
	}

	// Get the spec from the template's template field (spec.template.spec).
	currentSpec := currentTemplate.Spec.Template.Spec
	desiredSpec := desiredTemplate.Spec.Template.Spec

	log.Info("CanUpdateMachineSet check",
		"machineSet", req.Current.MachineSet.Name,
		"currentImage", currentSpec.Image,
		"desiredImage", desiredSpec.Image,
	)

	// Check if rolling-update-required fields have changed.
	if hasRollingUpdateRequiredChanges(currentSpec, desiredSpec) {
		log.Info("Rolling update required - Image/Initrd/KernelParams changed",
			"machineSet", req.Current.MachineSet.Name)
		resp.SetStatus(runtimehooksv1.ResponseStatusSuccess)
		resp.SetMessage("rolling update required: Image, Initrd, or KernelParams changed")
		return
	}

	// Build current and desired TartMachine objects for patch generation.
	currentMachine := &infrastructurev1alpha1.TartMachine{
		Spec: currentSpec,
	}
	desiredMachine := &infrastructurev1alpha1.TartMachine{
		Spec: desiredSpec,
	}

	patched, err := patchTartMachineSpec(currentMachine, desiredMachine)
	if err != nil {
		log.Error(err, "Failed to generate infrastructure machine template patch")
		resp.SetStatus(runtimehooksv1.ResponseStatusFailure)
		resp.SetMessage("failed to generate infrastructure machine template patch: " + err.Error())
		return
	}

	if patched != nil {
		// Build the patched template spec.
		patchedSpec := patched.Spec
		patchedTemplate := infrastructurev1alpha1.TartMachineTemplate{
			Spec: infrastructurev1alpha1.TartMachineTemplateSpec{
				Template: infrastructurev1alpha1.TartMachineTemplateResource{
					Spec: patchedSpec,
				},
			},
		}
		patchedTemplate.TypeMeta = currentTemplate.TypeMeta
		patchedTemplate.APIVersion = currentTemplate.APIVersion

		data, err := json.Marshal(patchedTemplate)
		if err == nil {
			resp.InfrastructureMachineTemplatePatch = runtimehooksv1.Patch{
				PatchType: runtimehooksv1.JSONMergePatchType,
				Patch:     data,
			}
		}

		resp.SetStatus(runtimehooksv1.ResponseStatusSuccess)
		resp.SetMessage("in-place update allowed for non-critical spec changes")
	} else {
		resp.SetStatus(runtimehooksv1.ResponseStatusSuccess)
		resp.SetMessage("no infrastructure machine template changes detected")
	}
}
