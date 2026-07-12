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
	"fmt"
	"time"

	"github.com/opencontainers/go-digest"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	slotdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/slot"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
)

// UpdatePlanInputは署名済みOSOnly Update Planの生成入力である。
type UpdatePlanInput struct {
	OperationID              string
	Machine                  *clusterv1.Machine
	TartMachine              *infrastructurev1beta1.TartMachine
	Host                     *infrastructurev1beta1.TartHost
	Deadline                 time.Time
	Manifest                 artifact.ValidatedManifest
	TargetImageDigest        string
	TargetArtifactGeneration uint64
}

// SignedUpdatePlanは保存可能なPlan、署名、Canonical JSON digestをまとめる。
type SignedUpdatePlan struct {
	Plan      agentprotocol.ValidatedPlan
	Signature agentprotocol.Signature
	Digest    digest.Digest
}

// BuildUpdatePlanはInactive Slot以外へ書けない署名済みPlanを生成する。
func BuildUpdatePlan(
	input UpdatePlanInput,
	keyID string,
	privateKey ed25519.PrivateKey,
) (SignedUpdatePlan, error) {
	if err := validateUpdatePlanInput(input); err != nil {
		return SignedUpdatePlan{}, err
	}
	active, err := slotdomain.Parse(string(input.TartMachine.Status.ActiveSlot))
	if err != nil {
		return SignedUpdatePlan{}, fmt.Errorf("parse active slot: %w", err)
	}
	target, err := active.Inactive()
	if err != nil {
		return SignedUpdatePlan{}, fmt.Errorf("select inactive slot: %w", err)
	}
	targetRoles, err := targetDiskRoles(target)
	if err != nil {
		return SignedUpdatePlan{}, err
	}
	manifestDigest, err := input.Manifest.Digest()
	if err != nil {
		return SignedUpdatePlan{}, fmt.Errorf("calculate Artifact Manifest digest: %w", err)
	}
	manifest := input.Manifest.Value()

	plan, err := agentprotocol.ValidatePlan(agentprotocol.Plan{
		APIVersion:    agentprotocol.APIVersion,
		OperationUID:  input.OperationID,
		HostUID:       string(input.Host.UID),
		OperationType: agentprotocol.OperationTypeUpdate,
		ActiveSlot:    string(active),
		Deadline:      input.Deadline.UTC(),
		RootDevice: agentprotocol.RootDevice{
			DeviceName:   input.Host.Spec.RootDeviceHints.DeviceName,
			SerialNumber: input.Host.Spec.RootDeviceHints.SerialNumber,
			WWN:          input.Host.Spec.RootDeviceHints.WWN,
			MinSizeBytes: input.Host.Spec.RootDeviceHints.MinSizeBytes,
		},
		Artifact: &agentprotocol.Artifact{
			Ref:            input.TartMachine.Spec.Image.Ref,
			ManifestDigest: manifestDigest.String(),
			Generation:     manifest.Generation,
		},
		AllowedTargetRoles: targetRoles,
		Steps: []agentprotocol.PlanStep{
			{Name: agentprotocol.StepWriteImage},
			{Name: agentprotocol.StepVerifyImage},
		},
	})
	if err != nil {
		return SignedUpdatePlan{}, fmt.Errorf("validate Update Plan: %w", err)
	}
	signature, err := agentprotocol.Sign(plan, keyID, privateKey)
	if err != nil {
		return SignedUpdatePlan{}, fmt.Errorf("sign Update Plan: %w", err)
	}
	planDigest, err := plan.Digest()
	if err != nil {
		return SignedUpdatePlan{}, fmt.Errorf("calculate Update Plan digest: %w", err)
	}
	return SignedUpdatePlan{
		Plan:      plan,
		Signature: signature,
		Digest:    planDigest,
	}, nil
}

func validateUpdatePlanInput(input UpdatePlanInput) error {
	switch {
	case input.Machine == nil:
		return fmt.Errorf("CAPI Machine is required")
	case input.TartMachine == nil:
		return fmt.Errorf("TartMachine is required")
	case input.Host == nil:
		return fmt.Errorf("TartHost is required")
	case input.OperationID == "":
		return fmt.Errorf("Operation ID is required")
	case input.Deadline.IsZero():
		return fmt.Errorf("Operation deadline is required")
	}
	manifest := input.Manifest.Value()
	switch {
	case manifest.PlatformProfile != input.TartMachine.Spec.PlatformProfile:
		return fmt.Errorf("Artifact Platform Profile does not match TartMachine")
	case manifest.PlatformProfile != input.Host.Spec.PlatformProfile:
		return fmt.Errorf("Artifact Platform Profile does not match TartHost")
	case manifest.Architecture != string(input.Host.Spec.Architecture):
		return fmt.Errorf("Artifact architecture does not match TartHost")
	case manifest.Kubernetes.Version != input.Machine.Spec.Version:
		return fmt.Errorf("Artifact Kubernetes version does not match desired Machine")
	case manifest.Generation != input.TargetArtifactGeneration:
		return fmt.Errorf("Artifact generation does not match Update Operation")
	case manifest.Image.Digest != input.TargetImageDigest:
		return fmt.Errorf("Artifact image digest does not match Update Operation")
	}
	return nil
}

func targetDiskRoles(target slotdomain.Slot) ([]agentprotocol.DiskRole, error) {
	switch target {
	case slotdomain.A:
		return []agentprotocol.DiskRole{
			agentprotocol.DiskRoleOSA,
			agentprotocol.DiskRoleVerityA,
		}, nil
	case slotdomain.B:
		return []agentprotocol.DiskRole{
			agentprotocol.DiskRoleOSB,
			agentprotocol.DiskRoleVerityB,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported inactive slot %q", target)
	}
}
