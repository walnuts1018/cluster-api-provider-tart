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
	"fmt"
	"time"

	"github.com/opencontainers/go-digest"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
)

// ProvisionPlanInput は署名済みProvision Planの生成に必要な検証済み入力である。
type ProvisionPlanInput struct {
	OperationID string
	Host        *infrastructurev1beta1.TartHost
	Machine     *infrastructurev1beta1.TartMachine
	MachineUID  string
	Deadline    time.Time
	Manifest    artifact.ValidatedManifest
}

// SignedProvisionPlan は保存可能なPlan、署名、Canonical JSON digestをまとめる。
type SignedProvisionPlan struct {
	Plan      agentprotocol.ValidatedPlan
	Signature agentprotocol.Signature
	Digest    digest.Digest
}

// BuildProvisionPlan はHost、Machine、Artifact Manifestから破壊操作の範囲を固定したPlanを生成する。
func BuildProvisionPlan(
	input ProvisionPlanInput,
	keyID string,
	privateKey ed25519.PrivateKey,
) (SignedProvisionPlan, error) {
	if input.Host == nil || input.Machine == nil {
		return SignedProvisionPlan{}, fmt.Errorf("TartHost and TartMachine are required")
	}
	manifest := input.Manifest.Value()
	if manifest.PlatformProfile != input.Machine.Spec.PlatformProfile ||
		manifest.PlatformProfile != input.Host.Spec.PlatformProfile {
		return SignedProvisionPlan{}, fmt.Errorf(
			"artifact Platform Profile %q does not match TartMachine and TartHost",
			manifest.PlatformProfile,
		)
	}
	if manifest.Architecture != string(input.Host.Spec.Architecture) {
		return SignedProvisionPlan{}, fmt.Errorf(
			"artifact architecture %q does not match TartHost architecture %q",
			manifest.Architecture,
			input.Host.Spec.Architecture,
		)
	}
	manifestDigest, err := input.Manifest.Digest()
	if err != nil {
		return SignedProvisionPlan{}, fmt.Errorf("calculate Artifact Manifest digest: %w", err)
	}

	validated, err := agentprotocol.ValidatePlan(agentprotocol.Plan{
		APIVersion:    agentprotocol.APIVersion,
		OperationUID:  input.OperationID,
		HostUID:       string(input.Host.UID),
		OperationType: agentprotocol.OperationTypeProvision,
		Deadline:      input.Deadline.UTC(),
		RootDevice: agentprotocol.RootDevice{
			DeviceName:   input.Host.Spec.RootDeviceHints.DeviceName,
			SerialNumber: input.Host.Spec.RootDeviceHints.SerialNumber,
			WWN:          input.Host.Spec.RootDeviceHints.WWN,
			MinSizeBytes: input.Host.Spec.RootDeviceHints.MinSizeBytes,
		},
		Artifact: agentprotocol.Artifact{
			Ref:            input.Machine.Spec.Image.Ref,
			ManifestDigest: manifestDigest.String(),
			Generation:     manifest.Generation,
		},
		AllowedTargetRoles: []agentprotocol.DiskRole{
			agentprotocol.DiskRoleBoot,
			agentprotocol.DiskRoleOSA,
			agentprotocol.DiskRoleOSB,
			agentprotocol.DiskRoleVerityA,
			agentprotocol.DiskRoleVerityB,
			agentprotocol.DiskRoleState,
			agentprotocol.DiskRoleData,
		},
		Steps: []agentprotocol.PlanStep{
			{Name: agentprotocol.StepWriteImage},
			{Name: agentprotocol.StepVerifyImage},
		},
		Bootstrap: &agentprotocol.BootstrapTarget{
			MachineUID: input.MachineUID,
			Format:     agentprotocol.BootstrapFormatCloud,
		},
	})
	if err != nil {
		return SignedProvisionPlan{}, fmt.Errorf("validate Provision Plan: %w", err)
	}
	signature, err := agentprotocol.Sign(validated, keyID, privateKey)
	if err != nil {
		return SignedProvisionPlan{}, fmt.Errorf("sign Provision Plan: %w", err)
	}
	planDigest, err := validated.Digest()
	if err != nil {
		return SignedProvisionPlan{}, fmt.Errorf("calculate Provision Plan digest: %w", err)
	}
	return SignedProvisionPlan{
		Plan:      validated,
		Signature: signature,
		Digest:    planDigest,
	}, nil
}
