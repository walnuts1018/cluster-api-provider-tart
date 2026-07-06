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

package inplaceupdate

import (
	"context"
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	application "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
)

func TestServiceStartはLiveStatusとDesiredSpecからWorkflowを開始する(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	liveMachine := updateTartMachine()
	host := updateHost()
	manifest := updateArtifactManifest(t)
	workflow := &recordingWorkflow{
		operation: &infrastructurev1beta1.TartHostOperation{
			ObjectMeta: metav1.ObjectMeta{Name: "operation-a", Namespace: "default"},
		},
	}
	service := NewService(
		fake.NewClientBuilder().WithScheme(scheme).WithObjects(liveMachine, host).Build(),
		staticManifestResolver{manifest: manifest},
		workflow,
	)

	request := updateRequest(t, liveMachine)
	started, err := service.Start(t.Context(), request)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if started.Name != "operation-a" {
		t.Fatalf("started Operation = %q, want operation-a", started.Name)
	}
	if workflow.input.TartMachine.Spec.Image.Ref != requestArtifactRef("b") {
		t.Fatalf("workflow image ref = %q, want desired ref", workflow.input.TartMachine.Spec.Image.Ref)
	}
	if workflow.input.TartMachine.Status.HostRef == nil ||
		workflow.input.TartMachine.Status.HostRef.UID != host.UID {
		t.Fatalf("workflow hostRef = %#v, want live status", workflow.input.TartMachine.Status.HostRef)
	}
	if workflow.input.TargetImageDigest != manifest.Value().Image.Digest {
		t.Fatalf("target image digest = %q, want Manifest digest", workflow.input.TargetImageDigest)
	}
}

type staticManifestResolver struct {
	manifest artifact.ValidatedManifest
}

func (resolver staticManifestResolver) ResolveManifest(
	context.Context,
	string,
) (artifact.ValidatedManifest, error) {
	return resolver.manifest, nil
}

type recordingWorkflow struct {
	input     application.WorkflowInput
	operation *infrastructurev1beta1.TartHostOperation
}

func (workflow *recordingWorkflow) Start(
	_ context.Context,
	input application.WorkflowInput,
) (*infrastructurev1beta1.TartHostOperation, error) {
	workflow.input = input
	return workflow.operation, nil
}

func updateRequest(
	t *testing.T,
	liveMachine *infrastructurev1beta1.TartMachine,
) *runtimehooksv1.UpdateMachineRequest {
	t.Helper()
	desired := liveMachine.DeepCopy()
	desired.Spec.Image.Ref = requestArtifactRef("b")
	raw, err := json.Marshal(desired)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return &runtimehooksv1.UpdateMachineRequest{
		Desired: runtimehooksv1.UpdateMachineRequestObjects{
			Machine: updateCAPIMachine(),
			InfrastructureMachine: runtime.RawExtension{
				Raw: raw,
			},
			BootstrapConfig: runtime.RawExtension{
				Raw: []byte(`{"spec":{"payload":"same"}}`),
			},
		},
	}
}

func updateTartMachine() *infrastructurev1beta1.TartMachine {
	return &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-a",
			Namespace: "default",
			UID:       types.UID("tart-machine-a"),
		},
		Spec: infrastructurev1beta1.TartMachineSpec{
			Image:           infrastructurev1beta1.ImageSpec{Ref: requestArtifactRef("a")},
			PlatformProfile: "amd64-uefi-ab/v1",
			UpdatePolicy: infrastructurev1beta1.UpdatePolicy{
				Mode: infrastructurev1beta1.UpdateModeInPlace,
			},
		},
		Status: infrastructurev1beta1.TartMachineStatus{
			HostRef: &infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "host-a",
				UID:       types.UID("host-a"),
			},
			ActiveSlot: infrastructurev1beta1.OSSlotA,
		},
	}
}

func updateHost() *infrastructurev1beta1.TartHost {
	return &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "host-a",
			Namespace: "default",
			UID:       types.UID("host-a"),
		},
		Spec: infrastructurev1beta1.TartHostSpec{
			Architecture:    infrastructurev1beta1.ArchitectureAMD64,
			PlatformProfile: "amd64-uefi-ab/v1",
		},
		Status: infrastructurev1beta1.TartHostStatus{
			Phase: infrastructurev1beta1.TartHostPhaseProvisioned,
		},
	}
}

func updateCAPIMachine() clusterv1.Machine {
	return clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-a",
			Namespace: "default",
			UID:       types.UID("capi-machine-a"),
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: "sample",
			Version:     "v1.34.0",
		},
	}
}

func updateArtifactManifest(t *testing.T) artifact.ValidatedManifest {
	t.Helper()
	manifest, err := artifact.Validate(artifact.Manifest{
		SchemaVersion:   artifact.SchemaVersion,
		MediaType:       artifact.MediaType,
		OS:              artifact.OS{Family: "ubuntu", Version: "24.04"},
		Architecture:    "amd64",
		Filesystem:      "ext4",
		Image:           artifact.Payload{Digest: requestDigest("b"), SizeBytes: 8 << 30},
		Verity:          artifact.Verity{Digest: requestDigest("c"), SizeBytes: 1 << 30, RootHash: requestHex("d")},
		StateSchema:     artifact.StateSchema{Min: 1, Max: 1},
		Kubernetes:      artifact.Kubernetes{Distribution: "kubeadm", Version: "v1.34.0"},
		Boot:            artifact.Boot{KernelDigest: requestDigest("e"), InitrdDigest: requestDigest("f")},
		Requirements:    artifact.Requirements{CPULevel: "x86-64-v1"},
		Generation:      2,
		PlatformProfile: "amd64-uefi-ab/v1",
	})
	if err != nil {
		t.Fatalf("artifact.Validate() error = %v", err)
	}
	return manifest
}

func requestArtifactRef(fill string) string {
	return "oci://registry.test.walnuts.dev/os/ubuntu@sha256:" + requestHex(fill)
}

func requestDigest(fill string) string {
	return "sha256:" + requestHex(fill)
}

func requestHex(fill string) string {
	result := ""
	for range 64 {
		result += fill
	}
	return result
}
