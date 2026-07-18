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

package initialprovisioning

import (
	"context"
	"testing"

	"github.com/opencontainers/go-digest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/artifact"
	completeprovisioning "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/complete_provisioning"
	appinitialprovisioning "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/provision_machine"
	sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"
	sharedworkflow "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/workflow"
)

func TestServiceStartResolvesOwnerMachineAndManifest(t *testing.T) {
	t.Parallel()

	testScheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(testScheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	if err := clusterv1.AddToScheme(testScheme); err != nil {
		t.Fatalf("clusterv1.AddToScheme() error = %v", err)
	}

	ownerMachine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owner-machine",
			Namespace: "default",
			UID:       types.UID("owner-machine-uid"),
		},
	}
	controller := true
	tartMachine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "infra-machine",
			Namespace: "default",
			UID:       types.UID("infra-machine-uid"),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: clusterv1.GroupVersion.String(),
				Kind:       "Machine",
				Name:       ownerMachine.Name,
				UID:        ownerMachine.UID,
				Controller: &controller,
			}},
		},
		Spec: infrastructurev1beta1.TartMachineSpec{
			Image: infrastructurev1beta1.ImageSpec{
				Ref: "oci://registry.test.walnuts.dev/os@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
		},
	}
	manifest := testValidatedManifest(t)
	workflow := &workflowStarterStub{
		result: appinitialprovisioning.Started{
			Host:      &infrastructurev1beta1.TartHost{},
			Operation: &infrastructurev1beta1.TartHostOperation{},
		},
	}
	service := NewService(
		fake.NewClientBuilder().WithScheme(testScheme).WithObjects(ownerMachine, tartMachine).Build(),
		staticManifestResolver{manifest: manifest},
		workflow,
		workflowCompleterStub{},
	)

	if _, err := service.Start(t.Context(), tartMachine); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if workflow.input == nil {
		t.Fatal("workflow input was not captured")
	}
	if workflow.input.MachineUID != string(ownerMachine.UID) {
		t.Fatalf("MachineUID = %q, want %q", workflow.input.MachineUID, ownerMachine.UID)
	}
	if workflow.input.Machine != tartMachine {
		t.Fatalf("Machine pointer was not forwarded")
	}
	if got := workflow.input.Manifest.Value().Image.Digest; got != manifest.Value().Image.Digest {
		t.Fatalf("Manifest digest = %q, want %q", got, manifest.Value().Image.Digest)
	}
}

func TestServiceStartRejectsMissingOwnerMachine(t *testing.T) {
	t.Parallel()

	testScheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(testScheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	if err := clusterv1.AddToScheme(testScheme); err != nil {
		t.Fatalf("clusterv1.AddToScheme() error = %v", err)
	}

	tartMachine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "infra-machine", Namespace: "default"},
		Spec: infrastructurev1beta1.TartMachineSpec{
			Image: infrastructurev1beta1.ImageSpec{
				Ref: "oci://registry.test.walnuts.dev/os@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
		},
	}
	service := NewService(
		fake.NewClientBuilder().WithScheme(testScheme).WithObjects(tartMachine).Build(),
		staticManifestResolver{manifest: testValidatedManifest(t)},
		&workflowStarterStub{},
		workflowCompleterStub{},
	)

	if _, err := service.Start(t.Context(), tartMachine); err == nil {
		t.Fatal("Start() succeeded without an owner Machine")
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

type workflowStarterStub struct {
	input  *appinitialprovisioning.Command
	result appinitialprovisioning.StartResult
}

func (stub *workflowStarterStub) Do(
	_ context.Context,
	input appinitialprovisioning.Command,
) sharedresult.Result[appinitialprovisioning.Event, sharedworkflow.Failure] {
	stub.input = &input
	switch result := stub.result.(type) {
	case appinitialprovisioning.Started:
		return sharedworkflow.Succeeded[appinitialprovisioning.Event](appinitialprovisioning.MachineProvisioningStarted{Result: result})
	case appinitialprovisioning.AllocationPending:
		return sharedworkflow.Succeeded[appinitialprovisioning.Event](appinitialprovisioning.HostAllocationPending{Result: result})
	default:
		return sharedworkflow.Failed[appinitialprovisioning.Event](sharedworkflow.DependencyFailure{Detail: "missing stub result"})
	}
}

type workflowCompleterStub struct{}

func (workflowCompleterStub) Do(context.Context, completeprovisioning.Command) sharedresult.Result[completeprovisioning.Event, sharedworkflow.Failure] {
	return sharedworkflow.Succeeded[completeprovisioning.Event](completeprovisioning.Event{})
}

func testValidatedManifest(t *testing.T) artifact.ValidatedManifest {
	t.Helper()
	validated, err := artifact.Validate(artifact.Manifest{
		SchemaVersion: artifact.SchemaVersion,
		MediaType:     artifact.MediaType,
		OS:            artifact.OS{Family: "ubuntu", Version: "24.04"},
		Architecture:  "amd64",
		Filesystem:    "ext4",
		Image: artifact.Payload{
			Digest:    digest.FromString("image").String(),
			SizeBytes: 8 * 1024 * 1024 * 1024,
		},
		Verity: artifact.Verity{
			Digest:    digest.FromString("verity").String(),
			SizeBytes: 1024 * 1024 * 1024,
			RootHash:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		StateSchema:     artifact.StateSchema{Min: 1, Max: 1},
		Kubernetes:      artifact.Kubernetes{Distribution: "kubeadm", LifecycleRuntime: "kubeadm.cluster.x-k8s.io/v1", Version: "v1.36.0"},
		Boot:            artifact.Boot{KernelDigest: digest.FromString("kernel").String(), InitrdDigest: digest.FromString("initrd").String()},
		Requirements:    artifact.Requirements{CPULevel: "x86-64-v1"},
		Generation:      1,
		PlatformProfile: "amd64-uefi-ab-ubuntu-24.04-kubeadm/v1",
	})
	if err != nil {
		t.Fatalf("artifact.Validate() error = %v", err)
	}
	return validated
}
