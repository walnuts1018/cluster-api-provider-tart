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

// HandleCanUpdateMachine handles the CanUpdateMachine hook.
// It compares the current and desired TartMachine specs and determines
// whether the change can be handled in-place or requires a rolling update.
func HandleCanUpdateMachine(ctx context.Context, req *runtimehooksv1.CanUpdateMachineRequest, resp *runtimehooksv1.CanUpdateMachineResponse) {
	log := ctrllog.FromContext(ctx)

	// Unmarshal the current and desired infrastructure machines.
	var currentTartMachine infrastructurev1alpha1.TartMachine
	if err := json.Unmarshal(req.Current.InfrastructureMachine.Raw, &currentTartMachine); err != nil {
		log.Error(err, "Failed to unmarshal current infrastructure machine")
		resp.SetStatus(runtimehooksv1.ResponseStatusFailure)
		resp.SetMessage("failed to unmarshal current infrastructure machine: " + err.Error())
		return
	}

	var desiredTartMachine infrastructurev1alpha1.TartMachine
	if err := json.Unmarshal(req.Desired.InfrastructureMachine.Raw, &desiredTartMachine); err != nil {
		log.Error(err, "Failed to unmarshal desired infrastructure machine")
		resp.SetStatus(runtimehooksv1.ResponseStatusFailure)
		resp.SetMessage("failed to unmarshal desired infrastructure machine: " + err.Error())
		return
	}

	log.Info("CanUpdateMachine check",
		"machine", req.Current.Machine.Name,
		"currentImage", currentTartMachine.Spec.Image,
		"desiredImage", desiredTartMachine.Spec.Image,
	)

	// Check if rolling-update-required fields have changed.
	if hasRollingUpdateRequiredChanges(currentTartMachine.Spec, desiredTartMachine.Spec) {
		log.Info("Rolling update required - Image/KernelParams changed",
			"machine", req.Current.Machine.Name)
		resp.SetStatus(runtimehooksv1.ResponseStatusSuccess)
		resp.SetMessage("rolling update required: Image or KernelParams changed")
		return
	}

	// Generate a patch for changes that can be handled in-place.
	patched, err := patchTartMachineSpec(&currentTartMachine, &desiredTartMachine)
	if err != nil {
		log.Error(err, "Failed to generate infrastructure machine patch")
		resp.SetStatus(runtimehooksv1.ResponseStatusFailure)
		resp.SetMessage("failed to generate infrastructure machine patch: " + err.Error())
		return
	}

	if patched != nil {
		patch := createPatch(patched)
		if patch != nil {
			resp.InfrastructureMachinePatch = *patch
		}
		resp.SetStatus(runtimehooksv1.ResponseStatusSuccess)
		resp.SetMessage("in-place update allowed for non-critical spec changes")
	} else {
		// No changes in the infrastructure machine spec at all.
		resp.SetStatus(runtimehooksv1.ResponseStatusSuccess)
		resp.SetMessage("no infrastructure machine changes detected")
	}
}
