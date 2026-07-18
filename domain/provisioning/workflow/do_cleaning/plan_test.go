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
	"crypto/rand"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	agentprotocol "github.com/walnuts1018/cluster-api-provider-tart/dto/agent"
)

func TestBuildCleaningPlanProducesValidatedSignedPlan(t *testing.T) {
	host := cleaningTestHost()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey() error = %v", err)
	}

	got, err := BuildCleaningPlan(CleaningPlanInput{
		OperationID:    "operation-uid",
		Host:           host,
		DeletionPolicy: infrastructurev1beta1.DeletionPolicyRetainData,
		Deadline:       time.Date(2026, 7, 12, 11, 0, 0, 0, time.UTC),
	}, "plan-key", privateKey)
	if err != nil {
		t.Fatalf("BuildCleaningPlan() error = %v", err)
	}
	plan := got.Plan.Value()
	if plan.OperationType != agentprotocol.OperationTypeClean {
		t.Fatalf("OperationType = %q, want Clean", plan.OperationType)
	}
	if plan.Artifact != nil {
		t.Fatalf("Artifact = %#v, want nil", plan.Artifact)
	}
	if plan.Bootstrap != nil {
		t.Fatalf("Bootstrap = %#v, want nil", plan.Bootstrap)
	}
	if len(plan.AllowedTargetRoles) != 6 || plan.AllowedTargetRoles[5] != agentprotocol.DiskRoleState {
		t.Fatalf("AllowedTargetRoles = %v", plan.AllowedTargetRoles)
	}
	if err := agentprotocol.VerifySignature(got.Plan, got.Signature, agentprotocol.StaticTrustStore{"plan-key": privateKey.Public().(ed25519.PublicKey)}); err != nil {
		t.Fatalf("VerifySignature() error = %v", err)
	}
}

func TestBuildCleaningPlanAllowsRetainStateWithoutTargets(t *testing.T) {
	host := cleaningTestHost()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey() error = %v", err)
	}

	got, err := BuildCleaningPlan(CleaningPlanInput{
		OperationID:    "operation-uid",
		Host:           host,
		DeletionPolicy: infrastructurev1beta1.DeletionPolicyRetainState,
		Deadline:       time.Date(2026, 7, 12, 11, 0, 0, 0, time.UTC),
	}, "plan-key", privateKey)
	if err != nil {
		t.Fatalf("BuildCleaningPlan() error = %v", err)
	}
	if roles := got.Plan.Value().AllowedTargetRoles; len(roles) != 0 {
		t.Fatalf("AllowedTargetRoles = %v, want empty", roles)
	}
}

func cleaningTestHost() *infrastructurev1beta1.TartHost {
	return &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "host-a",
			Namespace: "default",
			UID:       types.UID("host-a-uid"),
		},
		Spec: infrastructurev1beta1.TartHostSpec{
			RootDeviceHints: infrastructurev1beta1.RootDeviceHints{
				DeviceName:   "/dev/disk/by-id/root-disk",
				SerialNumber: "SERIAL-1",
				MinSizeBytes: 64 << 30,
			},
		},
	}
}
