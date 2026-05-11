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
	"encoding/json"
	"fmt"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
)

// CanUpdateInPlace determines whether the given TartMachine change can be handled in-place.
// It returns true if none of the rolling-update-required fields have changed.
func CanUpdateInPlace(current, desired *infrastructurev1alpha1.TartMachine) bool {
	if current == nil || desired == nil {
		return true
	}
	return !hasRollingUpdateRequiredChanges(current.Spec, desired.Spec)
}

// hasRollingUpdateRequiredChanges returns true if any spec field that requires a rolling
// update has changed between current and desired.
func hasRollingUpdateRequiredChanges(current, desired infrastructurev1alpha1.TartMachineSpec) bool {
	if current.Image != desired.Image {
		return true
	}
	if len(current.KernelParams) != len(desired.KernelParams) {
		return true
	}
	for i := range current.KernelParams {
		if current.KernelParams[i] != desired.KernelParams[i] {
			return true
		}
	}
	return false
}

// patchTartMachineSpec creates a JSONMergePatch from the current spec to the desired spec,
// excluding fields that require a rolling update. If any rolling-update-required fields differ,
// an empty patch is returned to signal that a rolling update is needed.
func patchTartMachineSpec(current, desired *infrastructurev1alpha1.TartMachine) (*infrastructurev1alpha1.TartMachine, error) {
	currentBytes, err := json.Marshal(current)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal current TartMachine: %w", err)
	}

	desiredBytes, err := json.Marshal(desired)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal desired TartMachine: %w", err)
	}

	patch, err := generateJSONMergePatch(currentBytes, desiredBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to generate patch: %w", err)
	}

	// If rolling-update-required fields changed, return empty patch to signal rolling update.
	if !CanUpdateInPlace(current, desired) {
		return &infrastructurev1alpha1.TartMachine{}, nil
	}

	// If there is no actual patch, return nil to indicate no changes needed.
	if len(patch) == 0 || string(patch) == "{}" {
		return nil, nil
	}

	// Apply the patch to the current object to produce the patched result.
	var patched infrastructurev1alpha1.TartMachine
	if err := json.Unmarshal(patch, &patched); err != nil {
		return nil, fmt.Errorf("failed to unmarshal patch: %w", err)
	}

	return &patched, nil
}

// generateJSONMergePatch generates a JSON merge patch (RFC 7386) from current to desired.
func generateJSONMergePatch(current, desired []byte) ([]byte, error) {
	var currentMap map[string]any
	var desiredMap map[string]any

	if err := json.Unmarshal(current, &currentMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal current: %w", err)
	}
	if err := json.Unmarshal(desired, &desiredMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal desired: %w", err)
	}

	patch := jsonMergePatch(currentMap, desiredMap)
	return json.Marshal(patch)
}

// jsonMergePatch recursively computes a JSON merge patch.
func jsonMergePatch(current, desired map[string]any) map[string]any {
	patch := map[string]any{}

	for key, desiredVal := range desired {
		currentVal, exists := current[key]
		if !exists {
			patch[key] = desiredVal
			continue
		}

		desiredMap, desiredIsMap := desiredVal.(map[string]any)
		currentMap, currentIsMap := currentVal.(map[string]any)

		if desiredIsMap && currentIsMap {
			subPatch := jsonMergePatch(currentMap, desiredMap)
			if len(subPatch) > 0 {
				patch[key] = subPatch
			}
			continue
		}

		if !jsonEqual(currentVal, desiredVal) {
			patch[key] = desiredVal
		}
	}

	return patch
}

// jsonEqual compares two JSON values for equality.
func jsonEqual(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			return false
		}
		return mapsEqual(av, bv)
	case []any:
		bv, ok := b.([]any)
		if !ok {
			return false
		}
		return slicesEqual(av, bv)
	case string, float64, bool, nil:
		return a == b
	default:
		return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	}
}

func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || !jsonEqual(av, bv) {
			return false
		}
	}
	return true
}

func slicesEqual(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !jsonEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

// createPatch creates a Patch object for the given response field.
func createPatch(obj *infrastructurev1alpha1.TartMachine) *runtimehooksv1.Patch {
	if obj == nil {
		return nil
	}

	data, err := json.Marshal(obj)
	if err != nil {
		return nil
	}

	return &runtimehooksv1.Patch{
		PatchType: runtimehooksv1.JSONMergePatchType,
		Patch:     data,
	}
}
