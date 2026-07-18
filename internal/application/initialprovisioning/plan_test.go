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
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
)

func TestBuildProvisionPlanProducesValidatedSignedPlan(t *testing.T) {
	t.Parallel()

	machine := testMachine()
	host := matchingProvisionHost()
	manifest := validatedProvisionManifest(t)
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	deadline := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)

	got, err := BuildProvisionPlan(ProvisionPlanInput{
		OperationID: "0197d640-8d00-7a65-b67f-3f7c42a6935f",
		Host:        host,
		Machine:     machine,
		MachineUID:  "capi-machine-uid",
		Deadline:    deadline,
		Manifest:    manifest,
	}, "provision-plan-v1", privateKey)
	if err != nil {
		t.Fatalf("BuildProvisionPlan() error = %v", err)
	}

	plan := got.Plan.Value()
	manifestDigest, err := manifest.Digest()
	if err != nil {
		t.Fatalf("Manifest.Digest() error = %v", err)
	}
	if plan.OperationType != agentprotocol.OperationTypeProvision ||
		plan.HostUID != string(host.UID) ||
		plan.Bootstrap == nil ||
		plan.Artifact == nil ||
		plan.Bootstrap.MachineUID != "capi-machine-uid" ||
		plan.Artifact.Ref != machine.Spec.Image.Ref ||
		plan.Artifact.ManifestDigest != manifestDigest.String() ||
		plan.Artifact.Generation != manifest.Value().Generation {
		t.Fatalf("Plan = %#v", plan)
	}
	if got.Digest.String() == "" || got.Signature.KeyID != "provision-plan-v1" {
		t.Fatalf("signed Plan metadata = %#v", got)
	}
	if err := agentprotocol.VerifySignature(
		got.Plan,
		got.Signature,
		agentprotocol.StaticTrustStore{"provision-plan-v1": privateKey.Public().(ed25519.PublicKey)},
	); err != nil {
		t.Fatalf("VerifySignature() error = %v", err)
	}
}

func TestBuildProvisionPlanRejectsManifestForDifferentProfile(t *testing.T) {
	t.Parallel()

	manifestValue := validatedProvisionManifest(t).Value()
	manifestValue.PlatformProfile = "amd64-uefi-ab-ubuntu-24.04-k3s/v1"
	manifestValue.Kubernetes.Distribution = "k3s"
	manifestValue.Kubernetes.LifecycleRuntime = "unsupported"
	manifest, err := artifact.Validate(manifestValue)
	if err != nil {
		t.Fatalf("artifact.Validate() error = %v", err)
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	_, err = BuildProvisionPlan(ProvisionPlanInput{
		OperationID: "0197d640-8d00-7a65-b67f-3f7c42a6935f",
		Host:        matchingProvisionHost(),
		Machine:     testMachine(),
		MachineUID:  "capi-machine-uid",
		Deadline:    time.Now().Add(time.Hour),
		Manifest:    manifest,
	}, "provision-plan-v1", privateKey)
	if err == nil {
		t.Fatal("BuildProvisionPlan() accepted a mismatched Platform Profile")
	}
}

func matchingProvisionHost() *infrastructurev1beta1.TartHost {
	host := &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{UID: types.UID("host-a-uid")},
		Spec: infrastructurev1beta1.TartHostSpec{
			Architecture:    infrastructurev1beta1.ArchitectureAMD64,
			Firmware:        infrastructurev1beta1.FirmwareUEFI,
			PlatformProfile: "amd64-uefi-ab-ubuntu-24.04-kubeadm/v1",
			RootDeviceHints: infrastructurev1beta1.RootDeviceHints{
				DeviceName:   "/dev/disk/by-id/wwn-test",
				SerialNumber: "disk-serial",
				MinSizeBytes: 64 * 1024 * 1024 * 1024,
			},
		},
	}
	return host
}

func validatedProvisionManifest(t *testing.T) artifact.ValidatedManifest {
	t.Helper()
	value := artifact.Manifest{
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
			RootHash:  strings.Repeat("a", 64),
		},
		StateSchema: artifact.StateSchema{Min: 1, Max: 1},
		Kubernetes: artifact.Kubernetes{
			Distribution:     "kubeadm",
			LifecycleRuntime: "kubeadm.cluster.x-k8s.io/v1",
			Version:          "v1.36.0",
		},
		Boot: artifact.Boot{
			KernelDigest: digest.FromString("kernel").String(),
			InitrdDigest: digest.FromString("initrd").String(),
		},
		Requirements:    artifact.Requirements{CPULevel: "x86-64-v1"},
		Generation:      1,
		PlatformProfile: "amd64-uefi-ab-ubuntu-24.04-kubeadm/v1",
	}
	validated, err := artifact.Validate(value)
	if err != nil {
		t.Fatalf("artifact.Validate() error = %v", err)
	}
	return validated
}
