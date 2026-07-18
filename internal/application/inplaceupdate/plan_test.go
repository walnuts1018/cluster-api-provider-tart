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
	"crypto/ed25519"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
)

func TestBuildUpdatePlanはInactiveSlotだけを許可する(t *testing.T) {
	tests := []struct {
		name       string
		activeSlot string
		wantOS     agentprotocol.DiskRole
		wantVerity agentprotocol.DiskRole
	}{
		{
			name:       "AからB",
			activeSlot: "A",
			wantOS:     agentprotocol.DiskRoleOSB,
			wantVerity: agentprotocol.DiskRoleVerityB,
		},
		{
			name:       "BからA",
			activeSlot: "B",
			wantOS:     agentprotocol.DiskRoleOSA,
			wantVerity: agentprotocol.DiskRoleVerityA,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := updatePlanInput(t)
			input.TartMachine.Status.ActiveSlot = infrastructurev1beta1.OSSlot(tt.activeSlot)

			signed, err := BuildUpdatePlan(input, "update-plan-key", testPrivateKey())
			if err != nil {
				t.Fatalf("BuildUpdatePlan() error = %v", err)
			}
			plan := signed.Plan.Value()
			if plan.OperationType != agentprotocol.OperationTypeUpdate {
				t.Fatalf("OperationType = %q, want Update", plan.OperationType)
			}
			if plan.ActiveSlot != tt.activeSlot {
				t.Fatalf("ActiveSlot = %q, want %q", plan.ActiveSlot, tt.activeSlot)
			}
			if len(plan.AllowedTargetRoles) != 3 ||
				plan.AllowedTargetRoles[0] != agentprotocol.DiskRoleBoot ||
				plan.AllowedTargetRoles[1] != tt.wantOS ||
				plan.AllowedTargetRoles[2] != tt.wantVerity {
				t.Fatalf("AllowedTargetRoles = %v, want [%s %s %s]",
					plan.AllowedTargetRoles, agentprotocol.DiskRoleBoot, tt.wantOS, tt.wantVerity)
			}
			if plan.Bootstrap != nil {
				t.Fatal("Bootstrap must be nil for OSOnly update")
			}
			if len(plan.Steps) != 2 ||
				plan.Steps[0].Name != agentprotocol.StepWriteImage ||
				plan.Steps[1].Name != agentprotocol.StepVerifyImage {
				t.Fatalf("Steps = %v, want WriteImage and VerifyImage", plan.Steps)
			}
			if signed.Digest.String() == "" || signed.Signature.KeyID != "update-plan-key" {
				t.Fatalf("signed plan = %#v", signed)
			}
		})
	}
}

func TestBuildUpdatePlanはManifestの不一致を拒否する(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*UpdatePlanInput)
	}{
		{
			name: "platform profile",
			mutate: func(input *UpdatePlanInput) {
				input.TartMachine.Spec.PlatformProfile = "amd64-uefi-ab/v2"
			},
		},
		{
			name: "architecture",
			mutate: func(input *UpdatePlanInput) {
				input.Host.Spec.Architecture = "arm64"
			},
		},
		{
			name: "Kubernetes version",
			mutate: func(input *UpdatePlanInput) {
				input.Machine.Spec.Version = "v1.35.0"
			},
		},
		{
			name: "artifact generation",
			mutate: func(input *UpdatePlanInput) {
				input.TargetArtifactGeneration = 3
			},
		},
		{
			name: "image digest",
			mutate: func(input *UpdatePlanInput) {
				input.TargetImageDigest = testDigest("f")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := updatePlanInput(t)
			tt.mutate(&input)
			if _, err := BuildUpdatePlan(input, "update-plan-key", testPrivateKey()); err == nil {
				t.Fatal("BuildUpdatePlan() error = nil, want manifest mismatch")
			}
		})
	}
}

func updatePlanInput(t *testing.T) UpdatePlanInput {
	t.Helper()
	start := updateInput()
	start.Host.Spec.Architecture = "amd64"
	start.Host.Spec.PlatformProfile = "amd64-uefi-ab-ubuntu-24.04-kubeadm/v1"
	start.Host.Spec.RootDeviceHints.DeviceName = "/dev/disk/by-id/test-root"
	start.Host.Spec.RootDeviceHints.SerialNumber = "SERIAL-1"
	start.Host.Spec.RootDeviceHints.MinSizeBytes = 64 << 30
	manifest := updateManifest(t)
	start.TargetImageDigest = manifest.Value().Image.Digest
	start.TargetArtifactGeneration = manifest.Value().Generation

	operation, err := BuildOperation(start)
	if err != nil {
		t.Fatalf("BuildOperation() error = %v", err)
	}
	return UpdatePlanInput{
		OperationID:              operation.Spec.OperationID,
		Machine:                  start.Machine,
		TartMachine:              start.TartMachine,
		Host:                     start.Host,
		Deadline:                 operation.Spec.Deadline.Time,
		Manifest:                 manifest,
		TargetImageDigest:        start.TargetImageDigest,
		TargetArtifactGeneration: start.TargetArtifactGeneration,
	}
}

func updateManifest(t *testing.T) artifact.ValidatedManifest {
	t.Helper()
	return updateManifestWithKubernetesVersion(t, "v1.36.0")
}

func updateManifestWithKubernetesVersion(t *testing.T, version string) artifact.ValidatedManifest {
	t.Helper()
	validated, err := artifact.Validate(artifact.Manifest{
		SchemaVersion:   artifact.SchemaVersion,
		MediaType:       artifact.MediaType,
		OS:              artifact.OS{Family: "ubuntu", Version: "24.04"},
		Architecture:    "amd64",
		Filesystem:      "ext4",
		Image:           artifact.Payload{Digest: testDigest("b"), SizeBytes: 8 << 30},
		Verity:          artifact.Verity{Digest: testDigest("c"), SizeBytes: 1 << 30, RootHash: strings.Repeat("d", 64)},
		StateSchema:     artifact.StateSchema{Min: 1, Max: 1},
		Kubernetes:      artifact.Kubernetes{Distribution: "kubeadm", LifecycleRuntime: "kubeadm.cluster.x-k8s.io/v1", Version: version},
		Boot:            artifact.Boot{KernelDigest: testDigest("e"), InitrdDigest: testDigest("f")},
		Requirements:    artifact.Requirements{CPULevel: "x86-64-v1"},
		Generation:      2,
		PlatformProfile: "amd64-uefi-ab-ubuntu-24.04-kubeadm/v1",
	})
	if err != nil {
		t.Fatalf("artifact.Validate() error = %v", err)
	}
	return validated
}

func testPrivateKey() ed25519.PrivateKey {
	seed := digest.FromString("task-08-update-plan-test-key").Encoded()
	return ed25519.NewKeyFromSeed([]byte(seed[:ed25519.SeedSize]))
}
