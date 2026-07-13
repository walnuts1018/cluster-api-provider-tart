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

package nodelifecycle

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	distribution "github.com/walnuts1018/cluster-api-provider-tart/internal/application/distributionlifecycle"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

func TestWorkflowは署名済みPlanだけを実行する(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	plan := validLifecyclePlan()
	validated, err := ValidatePlan(plan)
	if err != nil {
		t.Fatalf("ValidatePlan() error = %v", err)
	}
	signature, err := Sign(validated, "lifecycle-key", privateKey)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	runner := &recordingRunner{}
	workflow := NewWorkflow(agentprotocol.StaticTrustStore{"lifecycle-key": publicKey}, runner)

	result, err := workflow.RunSignedStep(
		t.Context(),
		SignedPlan{Plan: plan, Signature: signature},
		domain.StepPreflightCompleted,
	)
	if err != nil {
		t.Fatalf("RunSignedStep() error = %v", err)
	}
	if result != (distribution.StepResult{}) {
		t.Fatalf("RunSignedStep() result = %#v, want empty", result)
	}
	if runner.calls != 1 || runner.lastPlan.OperationID != "operation-1" || runner.lastStep != domain.StepPreflightCompleted {
		t.Fatalf("runner = %#v, want one PreflightCompleted call", runner)
	}
}

func TestWorkflowは改ざん済みPlanを実行しない(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	plan := validLifecyclePlan()
	validated, err := ValidatePlan(plan)
	if err != nil {
		t.Fatalf("ValidatePlan() error = %v", err)
	}
	signature, err := Sign(validated, "lifecycle-key", privateKey)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	plan.TargetVersion = "v1.36.1"
	runner := &recordingRunner{}
	workflow := NewWorkflow(agentprotocol.StaticTrustStore{"lifecycle-key": publicKey}, runner)

	if _, err := workflow.RunSignedStep(
		t.Context(),
		SignedPlan{Plan: plan, Signature: signature},
		domain.StepPreflightCompleted,
	); err == nil {
		t.Fatal("RunSignedStep() error = nil, want signature verification failure")
	}
	if runner.calls != 0 {
		t.Fatalf("runner.calls = %d, want 0", runner.calls)
	}
}

func TestWorkflowはSnapshotRefなしにKubeadmAppliedを実行しない(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	plan := validLifecyclePlan()
	plan.NodeRole = domain.NodeRoleControlPlane
	plan.Steps = []domain.Step{
		domain.StepPreflightCompleted,
		domain.StepSnapshotCreated,
		domain.StepTargetSlotWritten,
		domain.StepKubeadmApplied,
	}
	validated, err := ValidatePlan(plan)
	if err != nil {
		t.Fatalf("ValidatePlan() error = %v", err)
	}
	signature, err := Sign(validated, "lifecycle-key", privateKey)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	runner := &recordingRunner{}
	workflow := NewWorkflow(agentprotocol.StaticTrustStore{"lifecycle-key": publicKey}, runner)

	if _, err := workflow.RunSignedStep(
		t.Context(),
		SignedPlan{Plan: plan, Signature: signature},
		domain.StepKubeadmApplied,
	); err == nil {
		t.Fatal("RunSignedStep(KubeadmApplied) error = nil, want SnapshotRef required")
	}
	if runner.calls != 0 {
		t.Fatalf("runner.calls = %d, want 0", runner.calls)
	}
}

func TestBuildSignedPlanはDomainPlanから署名とDigestを生成する(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	deadline := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	domainPlan := domain.Plan{
		OperationID:    "operation-1",
		CurrentVersion: "v1.35.0",
		TargetVersion:  "v1.36.0",
		UpdateClass:    domain.UpdateClassKubernetesBinary,
		NodeRole:       domain.NodeRoleWorker,
		Steps:          []domain.Step{domain.StepPreflightCompleted, domain.StepKubeadmApplied},
	}

	built, err := BuildSignedPlan(domainPlan, deadline, "lifecycle-key", privateKey)
	if err != nil {
		t.Fatalf("BuildSignedPlan() error = %v", err)
	}
	if err := VerifySignature(built.Plan, built.Signature, agentprotocol.StaticTrustStore{"lifecycle-key": publicKey}); err != nil {
		t.Fatalf("VerifySignature() error = %v", err)
	}
	digest, err := built.Plan.Digest()
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	if built.Digest != digest {
		t.Fatalf("digest = %q, want %q", built.Digest, digest)
	}

	next, err := BuildSignedPlan(domainPlan, deadline.Add(time.Minute), "lifecycle-key", privateKey)
	if err != nil {
		t.Fatalf("BuildSignedPlan(changed deadline) error = %v", err)
	}
	if next.Digest == built.Digest {
		t.Fatal("BuildSignedPlan() digest did not change after deadline changed")
	}
}

func TestValidatePlanは署名対象Planの危険な入力を拒否する(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Plan)
	}{
		{name: "unsupported apiVersion", mutate: func(plan *Plan) { plan.APIVersion = "v1" }},
		{name: "missing operationID", mutate: func(plan *Plan) { plan.OperationID = "" }},
		{name: "missing deadline", mutate: func(plan *Plan) { plan.Deadline = time.Time{} }},
		{name: "unknown step", mutate: func(plan *Plan) { plan.Steps = []domain.Step{"Shell"} }},
		{name: "unknown update class", mutate: func(plan *Plan) { plan.UpdateClass = "Unknown" }},
		{name: "unknown node role", mutate: func(plan *Plan) { plan.NodeRole = "Unknown" }},
		{name: "duplicate step", mutate: func(plan *Plan) {
			plan.Steps = []domain.Step{domain.StepPreflightCompleted, domain.StepPreflightCompleted}
		}},
		{name: "state migration without snapshot", mutate: func(plan *Plan) {
			plan.UpdateClass = domain.UpdateClassStateMigration
			plan.SnapshotRef = ""
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := validLifecyclePlan()
			test.mutate(&plan)
			if _, err := ValidatePlan(plan); err == nil {
				t.Fatal("ValidatePlan() error = nil, want validation error")
			}
		})
	}
}

func validLifecyclePlan() Plan {
	return Plan{
		APIVersion:     APIVersion,
		OperationID:    "operation-1",
		CurrentVersion: "v1.35.0",
		TargetVersion:  "v1.36.0",
		UpdateClass:    domain.UpdateClassKubernetesBinary,
		NodeRole:       domain.NodeRoleWorker,
		Deadline:       time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
		Steps: []domain.Step{
			domain.StepPreflightCompleted,
			domain.StepTargetSlotWritten,
			domain.StepKubeadmApplied,
			domain.StepTargetSlotBooted,
			domain.StepHealthVerified,
			domain.StepCommitted,
		},
	}
}

type recordingRunner struct {
	calls    int
	lastPlan domain.Plan
	lastStep domain.Step
}

func (runner *recordingRunner) RunStep(
	_ context.Context,
	plan domain.Plan,
	step domain.Step,
) (distribution.StepResult, error) {
	runner.calls++
	runner.lastPlan = plan
	runner.lastStep = step
	return distribution.StepResult{}, nil
}
