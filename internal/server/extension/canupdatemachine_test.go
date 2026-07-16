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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

const (
	extensionCurrentVersion = "v1.34.0"
	extensionTargetVersion  = "v1.35.0"
)

func TestHandleCanUpdateMachineはOSOnly差分だけをPatchする(t *testing.T) {
	tests := []struct {
		name              string
		mutate            func(*runtimehooksv1.CanUpdateMachineRequest)
		distributionGates DistributionLifecycleFeatureGates
		wantPatch         bool
	}{
		{
			name: "image ref",
			mutate: func(request *runtimehooksv1.CanUpdateMachineRequest) {
				machine := decodeTestTartMachine(t, request.Desired.InfrastructureMachine)
				machine.Spec.Image.Ref = extensionArtifactRef("b")
				request.Desired.InfrastructureMachine = rawExtension(t, machine)
			},
			wantPatch: true,
		},
		{
			name: "update policy",
			mutate: func(request *runtimehooksv1.CanUpdateMachineRequest) {
				machine := decodeTestTartMachine(t, request.Desired.InfrastructureMachine)
				machine.Spec.UpdatePolicy.Mode = infrastructurev1beta1.UpdateModeInPlace
				request.Desired.InfrastructureMachine = rawExtension(t, machine)
			},
			wantPatch: true,
		},
		{
			name: "Kubernetes version",
			mutate: func(request *runtimehooksv1.CanUpdateMachineRequest) {
				request.Desired.Machine.Spec.Version = extensionTargetVersion
			},
		},
		{
			name: "Kubernetes version with distribution lifecycle gate",
			mutate: func(request *runtimehooksv1.CanUpdateMachineRequest) {
				request.Desired.Machine.Spec.Version = extensionTargetVersion
			},
			distributionGates: DistributionLifecycleFeatureGates{Worker: true},
			wantPatch:         true,
		},
		{
			name: "bootstrap payload",
			mutate: func(request *runtimehooksv1.CanUpdateMachineRequest) {
				request.Desired.BootstrapConfig = bootstrapConfig(`{"payload":"changed"}`)
			},
		},
		{
			name: "platform profile",
			mutate: func(request *runtimehooksv1.CanUpdateMachineRequest) {
				machine := decodeTestTartMachine(t, request.Desired.InfrastructureMachine)
				machine.Spec.PlatformProfile = "amd64-uefi-ab/v2"
				request.Desired.InfrastructureMachine = rawExtension(t, machine)
			},
		},
		{
			name: "host selector",
			mutate: func(request *runtimehooksv1.CanUpdateMachineRequest) {
				machine := decodeTestTartMachine(t, request.Desired.InfrastructureMachine)
				machine.Spec.HostSelector.MatchLabels["rack"] = "rack-b"
				request.Desired.InfrastructureMachine = rawExtension(t, machine)
			},
		},
		{
			name: "provider ID",
			mutate: func(request *runtimehooksv1.CanUpdateMachineRequest) {
				machine := decodeTestTartMachine(t, request.Desired.InfrastructureMachine)
				machine.Spec.ProviderID = "tart://host-2"
				request.Desired.InfrastructureMachine = rawExtension(t, machine)
			},
		},
		{
			name: "deletion policy",
			mutate: func(request *runtimehooksv1.CanUpdateMachineRequest) {
				machine := decodeTestTartMachine(t, request.Desired.InfrastructureMachine)
				machine.Spec.DeletionPolicy = infrastructurev1beta1.DeletionPolicyRetainData
				request.Desired.InfrastructureMachine = rawExtension(t, machine)
			},
		},
		{
			name: "image ref with rejected version",
			mutate: func(request *runtimehooksv1.CanUpdateMachineRequest) {
				request.Desired.Machine.Spec.Version = extensionTargetVersion
				machine := decodeTestTartMachine(t, request.Desired.InfrastructureMachine)
				machine.Spec.Image.Ref = extensionArtifactRef("b")
				request.Desired.InfrastructureMachine = rawExtension(t, machine)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := machineUpdateRequest(t)
			tt.mutate(request)
			response := &runtimehooksv1.CanUpdateMachineResponse{}
			handler := NewCanUpdateMachineHandler(
				newTestTargetSupportChecker(
					t,
					UpdateTargetFeatureGates{Worker: true, MultiControlPlane: true, SingleControlPlane: true},
					tt.distributionGates,
					request.Current.Machine.DeepCopy(),
				),
			)

			handler.Handle(t.Context(), request, response)

			if response.Status != runtimehooksv1.ResponseStatusSuccess {
				t.Fatalf("Status = %q, want %q; message=%q",
					response.Status, runtimehooksv1.ResponseStatusSuccess, response.Message)
			}
			if response.InfrastructureMachinePatch.IsDefined() != tt.wantPatch {
				t.Fatalf("InfrastructureMachinePatch.IsDefined() = %t, want %t; message=%q",
					response.InfrastructureMachinePatch.IsDefined(), tt.wantPatch, response.Message)
			}
			if response.MachinePatch.IsDefined() {
				t.Error("MachinePatch must not be defined")
			}
			if response.BootstrapConfigPatch.IsDefined() {
				t.Error("BootstrapConfigPatch must not be defined")
			}
			if tt.wantPatch {
				assertOSOnlyPatch(t, response.InfrastructureMachinePatch)
			}
		})
	}
}

func TestHandleCanUpdateMachineは無効な対象ではPatchを返さず通常置換へfallbackする(t *testing.T) {
	tests := []struct {
		name              string
		mutate            func(*runtimehooksv1.CanUpdateMachineRequest)
		gates             UpdateTargetFeatureGates
		distributionGates DistributionLifecycleFeatureGates
		objects           []runtime.Object
	}{
		{
			name:  "worker gate無効",
			gates: UpdateTargetFeatureGates{},
		},
		{
			name: "worker distribution lifecycle gate無効",
			mutate: func(request *runtimehooksv1.CanUpdateMachineRequest) {
				request.Desired.Machine.Spec.Version = extensionTargetVersion
			},
			gates: UpdateTargetFeatureGates{Worker: true, MultiControlPlane: true, SingleControlPlane: true},
		},
		{
			name: "single control plane gate無効",
			mutate: func(request *runtimehooksv1.CanUpdateMachineRequest) {
				markControlPlaneMachine(&request.Current.Machine)
				markControlPlaneMachine(&request.Desired.Machine)
			},
			gates: UpdateTargetFeatureGates{Worker: true, MultiControlPlane: true},
		},
		{
			name: "multi control plane gate無効",
			mutate: func(request *runtimehooksv1.CanUpdateMachineRequest) {
				markControlPlaneMachine(&request.Current.Machine)
				markControlPlaneMachine(&request.Desired.Machine)
			},
			gates: UpdateTargetFeatureGates{Worker: true, SingleControlPlane: true},
			objects: []runtime.Object{
				controlPlaneMachine("cp-2"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := machineUpdateRequest(t)
			machine := decodeTestTartMachine(t, request.Desired.InfrastructureMachine)
			machine.Spec.Image.Ref = extensionArtifactRef("b")
			request.Desired.InfrastructureMachine = rawExtension(t, machine)
			if tt.mutate != nil {
				tt.mutate(request)
			}
			response := &runtimehooksv1.CanUpdateMachineResponse{}
			handler := NewCanUpdateMachineHandler(
				newTestTargetSupportChecker(t, tt.gates, tt.distributionGates, append([]runtime.Object{
					request.Current.Machine.DeepCopy(),
				}, tt.objects...)...),
			)

			handler.Handle(t.Context(), request, response)

			if response.Status != runtimehooksv1.ResponseStatusSuccess {
				t.Fatalf("Status = %q, want %q; message=%q",
					response.Status, runtimehooksv1.ResponseStatusSuccess, response.Message)
			}
			if response.InfrastructureMachinePatch.IsDefined() {
				t.Fatalf("InfrastructureMachinePatch.IsDefined() = true, want false; message=%q", response.Message)
			}
		})
	}
}

func TestHandleCanUpdateMachineは不正なInfraMachineをFailureにする(t *testing.T) {
	request := machineUpdateRequest(t)
	request.Desired.InfrastructureMachine.Raw = []byte(`{`)
	response := &runtimehooksv1.CanUpdateMachineResponse{}
	handler := NewCanUpdateMachineHandler(
		newTestTargetSupportChecker(
			t,
			UpdateTargetFeatureGates{Worker: true, MultiControlPlane: true, SingleControlPlane: true},
			DistributionLifecycleFeatureGates{},
			request.Current.Machine.DeepCopy(),
		),
	)

	handler.Handle(t.Context(), request, response)

	if response.Status != runtimehooksv1.ResponseStatusFailure {
		t.Fatalf("Status = %q, want %q", response.Status, runtimehooksv1.ResponseStatusFailure)
	}
}

func machineUpdateRequest(t *testing.T) *runtimehooksv1.CanUpdateMachineRequest {
	t.Helper()

	machine := clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-1",
			Namespace: "default",
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: "sample",
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: "sample",
			Version:     extensionCurrentVersion,
			ProviderID:  "tart://host-1",
		},
	}
	tartMachine := infrastructurev1beta1.TartMachine{
		Spec: infrastructurev1beta1.TartMachineSpec{
			ProviderID:      "tart://host-1",
			Image:           infrastructurev1beta1.ImageSpec{Ref: extensionArtifactRef("a")},
			PlatformProfile: "amd64-uefi-ab/v1",
			HostSelector: infrastructurev1beta1.HostSelector{
				MatchLabels: map[string]string{"rack": "rack-a"},
			},
			UpdatePolicy:   infrastructurev1beta1.UpdatePolicy{Mode: infrastructurev1beta1.UpdateModeReplace},
			DeletionPolicy: infrastructurev1beta1.DeletionPolicyWipeAll,
		},
	}

	return &runtimehooksv1.CanUpdateMachineRequest{
		Current: runtimehooksv1.CanUpdateMachineRequestObjects{
			Machine:               machine,
			InfrastructureMachine: rawExtension(t, tartMachine),
			BootstrapConfig:       bootstrapConfig(`{"payload":"same"}`),
		},
		Desired: runtimehooksv1.CanUpdateMachineRequestObjects{
			Machine:               *machine.DeepCopy(),
			InfrastructureMachine: rawExtension(t, tartMachine.DeepCopy()),
			BootstrapConfig:       bootstrapConfig(`{"payload":"same"}`),
		},
	}
}

func rawExtension(t *testing.T, object any) runtime.RawExtension {
	t.Helper()
	data, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return runtime.RawExtension{Raw: data}
}

func bootstrapConfig(spec string) runtime.RawExtension {
	return runtime.RawExtension{
		Raw: []byte(`{"apiVersion":"bootstrap.cluster.x-k8s.io/v1beta2","kind":"KubeadmConfig","spec":` + spec + `}`),
	}
}

func decodeTestTartMachine(t *testing.T, extension runtime.RawExtension) *infrastructurev1beta1.TartMachine {
	t.Helper()
	var machine infrastructurev1beta1.TartMachine
	if err := json.Unmarshal(extension.Raw, &machine); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return &machine
}

func assertOSOnlyPatch(t *testing.T, patch runtimehooksv1.Patch) {
	t.Helper()
	if patch.PatchType != runtimehooksv1.JSONMergePatchType {
		t.Fatalf("PatchType = %q, want %q", patch.PatchType, runtimehooksv1.JSONMergePatchType)
	}
	var value map[string]any
	if err := json.Unmarshal(patch.Patch, &value); err != nil {
		t.Fatalf("json.Unmarshal(patch) error = %v", err)
	}
	if len(value) != 1 || value["spec"] == nil {
		t.Fatalf("patch = %s, want only spec", patch.Patch)
	}
	spec, ok := value["spec"].(map[string]any)
	if !ok {
		t.Fatalf("patch spec = %#v, want object", value["spec"])
	}
	for key := range spec {
		if key != "image" && key != "updatePolicy" {
			t.Errorf("patch contains rejected spec field %q: %s", key, patch.Patch)
		}
	}
}

func extensionArtifactRef(fill string) string {
	value := ""
	for range 64 {
		value += fill
	}
	return "oci://registry.test.walnuts.dev/os/ubuntu@sha256:" + value
}

func newTestTargetSupportChecker(
	t *testing.T,
	gates UpdateTargetFeatureGates,
	distributionGates DistributionLifecycleFeatureGates,
	objects ...runtime.Object,
) *TargetSupportChecker {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := clusterv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	builder := fake.NewClientBuilder().WithScheme(scheme)
	if len(objects) > 0 {
		builder = builder.WithRuntimeObjects(objects...)
	}
	return NewTargetSupportChecker(builder.Build(), gates, distributionGates)
}

func markControlPlaneMachine(machine *clusterv1.Machine) {
	if machine.Labels == nil {
		machine.Labels = map[string]string{}
	}
	machine.Labels[clusterv1.MachineControlPlaneLabel] = ""
	machine.Labels[clusterv1.MachineControlPlaneNameLabel] = "sample-control-plane"
}

func controlPlaneMachine(name string) *clusterv1.Machine {
	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:             "sample",
				clusterv1.MachineControlPlaneLabel:     "",
				clusterv1.MachineControlPlaneNameLabel: "sample-control-plane",
			},
		},
		Spec: clusterv1.MachineSpec{ClusterName: "sample"},
	}
	return machine
}
