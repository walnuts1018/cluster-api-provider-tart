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

package cleaning

import (
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/opencontainers/go-digest"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

type CleaningPlanInput struct {
	OperationID    string
	Host           *infrastructurev1beta1.TartHost
	DeletionPolicy infrastructurev1beta1.DeletionPolicy
	Deadline       time.Time
}

type SignedCleaningPlan struct {
	Plan      agentprotocol.ValidatedPlan
	Signature agentprotocol.Signature
	Digest    digest.Digest
}

func BuildCleaningPlan(
	input CleaningPlanInput,
	keyID string,
	privateKey ed25519.PrivateKey,
) (SignedCleaningPlan, error) {
	if input.Host == nil {
		return SignedCleaningPlan{}, fmt.Errorf("TartHost is required")
	}
	if input.OperationID == "" {
		return SignedCleaningPlan{}, fmt.Errorf("Operation ID is required")
	}
	if input.Deadline.IsZero() {
		return SignedCleaningPlan{}, fmt.Errorf("Operation deadline is required")
	}
	allowed, err := AllowedTargetRoles(input.DeletionPolicy)
	if err != nil {
		return SignedCleaningPlan{}, err
	}
	operationType := agentprotocol.OperationTypeClean
	if input.DeletionPolicy == infrastructurev1beta1.DeletionPolicyWipeAll {
		operationType = agentprotocol.OperationTypeWipeAll
	}
	plan, err := agentprotocol.ValidatePlan(agentprotocol.Plan{
		APIVersion:    agentprotocol.APIVersion,
		OperationUID:  input.OperationID,
		HostUID:       string(input.Host.UID),
		OperationType: operationType,
		Deadline:      input.Deadline.UTC(),
		RootDevice: agentprotocol.RootDevice{
			DeviceName:   input.Host.Spec.RootDeviceHints.DeviceName,
			SerialNumber: input.Host.Spec.RootDeviceHints.SerialNumber,
			WWN:          input.Host.Spec.RootDeviceHints.WWN,
			MinSizeBytes: input.Host.Spec.RootDeviceHints.MinSizeBytes,
		},
		AllowedTargetRoles: allowed,
		Steps: []agentprotocol.PlanStep{
			{Name: agentprotocol.StepWriteImage},
			{Name: agentprotocol.StepVerifyImage},
		},
	})
	if err != nil {
		return SignedCleaningPlan{}, fmt.Errorf("validate Cleaning Plan: %w", err)
	}
	signature, err := agentprotocol.Sign(plan, keyID, privateKey)
	if err != nil {
		return SignedCleaningPlan{}, fmt.Errorf("sign Cleaning Plan: %w", err)
	}
	planDigest, err := plan.Digest()
	if err != nil {
		return SignedCleaningPlan{}, fmt.Errorf("calculate Cleaning Plan digest: %w", err)
	}
	return SignedCleaningPlan{
		Plan:      plan,
		Signature: signature,
		Digest:    planDigest,
	}, nil
}
