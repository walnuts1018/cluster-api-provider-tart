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
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

func TestHandleCanUpdateMachineSetはOSOnly差分だけをPatchする(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*runtimehooksv1.CanUpdateMachineSetRequest)
		wantPatch bool
	}{
		{
			name: "image ref",
			mutate: func(request *runtimehooksv1.CanUpdateMachineSetRequest) {
				template := decodeTestTartMachineTemplate(t, request.Desired.InfrastructureMachineTemplate)
				template.Spec.Template.Spec.Image.Ref = extensionArtifactRef("b")
				request.Desired.InfrastructureMachineTemplate = rawExtension(t, template)
			},
			wantPatch: true,
		},
		{
			name: "update policy",
			mutate: func(request *runtimehooksv1.CanUpdateMachineSetRequest) {
				template := decodeTestTartMachineTemplate(t, request.Desired.InfrastructureMachineTemplate)
				template.Spec.Template.Spec.UpdatePolicy.Mode = infrastructurev1beta1.UpdateModeInPlace
				request.Desired.InfrastructureMachineTemplate = rawExtension(t, template)
			},
			wantPatch: true,
		},
		{
			name: "Kubernetes version",
			mutate: func(request *runtimehooksv1.CanUpdateMachineSetRequest) {
				request.Desired.MachineSet.Spec.Template.Spec.Version = "v1.35.0"
			},
		},
		{
			name: "bootstrap template",
			mutate: func(request *runtimehooksv1.CanUpdateMachineSetRequest) {
				request.Desired.BootstrapConfigTemplate = bootstrapConfigTemplate(`{"payload":"changed"}`)
			},
		},
		{
			name: "platform profile",
			mutate: func(request *runtimehooksv1.CanUpdateMachineSetRequest) {
				template := decodeTestTartMachineTemplate(t, request.Desired.InfrastructureMachineTemplate)
				template.Spec.Template.Spec.PlatformProfile = "amd64-uefi-ab/v2"
				request.Desired.InfrastructureMachineTemplate = rawExtension(t, template)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := machineSetUpdateRequest(t)
			tt.mutate(request)
			response := &runtimehooksv1.CanUpdateMachineSetResponse{}

			HandleCanUpdateMachineSet(t.Context(), request, response)

			if response.Status != runtimehooksv1.ResponseStatusSuccess {
				t.Fatalf("Status = %q, want %q; message=%q",
					response.Status, runtimehooksv1.ResponseStatusSuccess, response.Message)
			}
			if response.InfrastructureMachineTemplatePatch.IsDefined() != tt.wantPatch {
				t.Fatalf("InfrastructureMachineTemplatePatch.IsDefined() = %t, want %t; message=%q",
					response.InfrastructureMachineTemplatePatch.IsDefined(), tt.wantPatch, response.Message)
			}
			if response.MachineSetPatch.IsDefined() {
				t.Error("MachineSetPatch must not be defined")
			}
			if response.BootstrapConfigTemplatePatch.IsDefined() {
				t.Error("BootstrapConfigTemplatePatch must not be defined")
			}
			if tt.wantPatch {
				assertTemplateOSOnlyPatch(t, response.InfrastructureMachineTemplatePatch)
			}
		})
	}
}

func machineSetUpdateRequest(t *testing.T) *runtimehooksv1.CanUpdateMachineSetRequest {
	t.Helper()

	machineSet := clusterv1.MachineSet{
		Spec: clusterv1.MachineSetSpec{
			ClusterName: "sample",
			Template: clusterv1.MachineTemplateSpec{
				Spec: clusterv1.MachineSpec{
					ClusterName: "sample",
					Version:     "v1.34.0",
				},
			},
		},
	}
	template := infrastructurev1beta1.TartMachineTemplate{
		Spec: infrastructurev1beta1.TartMachineTemplateSpec{
			Template: infrastructurev1beta1.TartMachineTemplateResource{
				Spec: infrastructurev1beta1.TartMachineTemplateResourceSpec{
					Image:           infrastructurev1beta1.ImageSpec{Ref: extensionArtifactRef("a")},
					PlatformProfile: "amd64-uefi-ab/v1",
					UpdatePolicy: infrastructurev1beta1.UpdatePolicy{
						Mode: infrastructurev1beta1.UpdateModeReplace,
					},
					DeletionPolicy: infrastructurev1beta1.DeletionPolicyWipeAll,
				},
			},
		},
	}

	return &runtimehooksv1.CanUpdateMachineSetRequest{
		Current: runtimehooksv1.CanUpdateMachineSetRequestObjects{
			MachineSet:                    machineSet,
			InfrastructureMachineTemplate: rawExtension(t, template),
			BootstrapConfigTemplate:       bootstrapConfigTemplate(`{"payload":"same"}`),
		},
		Desired: runtimehooksv1.CanUpdateMachineSetRequestObjects{
			MachineSet:                    *machineSet.DeepCopy(),
			InfrastructureMachineTemplate: rawExtension(t, template.DeepCopy()),
			BootstrapConfigTemplate:       bootstrapConfigTemplate(`{"payload":"same"}`),
		},
	}
}

func bootstrapConfigTemplate(spec string) runtime.RawExtension {
	return runtime.RawExtension{
		Raw: []byte(`{"apiVersion":"bootstrap.cluster.x-k8s.io/v1beta2","kind":"KubeadmConfigTemplate","spec":{"template":{"spec":` + spec + `}}}`),
	}
}

func decodeTestTartMachineTemplate(
	t *testing.T,
	extension runtime.RawExtension,
) *infrastructurev1beta1.TartMachineTemplate {
	t.Helper()
	var template infrastructurev1beta1.TartMachineTemplate
	if err := json.Unmarshal(extension.Raw, &template); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return &template
}

func assertTemplateOSOnlyPatch(t *testing.T, patch runtimehooksv1.Patch) {
	t.Helper()
	var value struct {
		Spec struct {
			Template struct {
				Spec map[string]any `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(patch.Patch, &value); err != nil {
		t.Fatalf("json.Unmarshal(patch) error = %v", err)
	}
	if len(value.Spec.Template.Spec) == 0 {
		t.Fatalf("patch = %s, want template spec", patch.Patch)
	}
	for key := range value.Spec.Template.Spec {
		if key != "image" && key != "updatePolicy" {
			t.Errorf("patch contains rejected template spec field %q: %s", key, patch.Patch)
		}
	}
}
