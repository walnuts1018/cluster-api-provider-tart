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

package distributionlifecycle

import (
	"slices"
	"testing"
)

func TestBuildPlanはWorkerではSnapshotなしのLifecycleStepを作る(t *testing.T) {
	plan, err := BuildPlan(PlanInput{
		Distribution:   DistributionKubeadm,
		OperationID:    "operation-1",
		CurrentVersion: "v1.34.0",
		TargetVersion:  "v1.35.0",
		UpdateClass:    UpdateClassKubernetesBinary,
		NodeRole:       NodeRoleWorker,
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if slices.Contains(plan.Steps, StepSnapshotCreated) {
		t.Fatalf("worker plan steps = %v, want no SnapshotCreated", plan.Steps)
	}
	want := []Step{
		StepPreflightCompleted,
		StepTargetSlotWritten,
		StepDistributionApplied,
		StepTargetSlotBooted,
		StepHealthVerified,
		StepCommitted,
	}
	if !slices.Equal(plan.Steps, want) {
		t.Fatalf("worker plan steps = %v, want %v", plan.Steps, want)
	}
}

func TestBuildPlanはControlPlaneではSnapshotをApply前に要求する(t *testing.T) {
	plan, err := BuildPlan(PlanInput{
		Distribution:   DistributionKubeadm,
		OperationID:    "operation-1",
		CurrentVersion: "v1.34.0",
		TargetVersion:  "v1.35.0",
		UpdateClass:    UpdateClassKubernetesBinary,
		NodeRole:       NodeRoleControlPlane,
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	want := []Step{
		StepPreflightCompleted,
		StepSnapshotCreated,
		StepTargetSlotWritten,
		StepDistributionApplied,
		StepTargetSlotBooted,
		StepHealthVerified,
		StepCommitted,
	}
	if !slices.Equal(plan.Steps, want) {
		t.Fatalf("control plane plan steps = %v, want %v", plan.Steps, want)
	}
}

func TestPlanReadyForStepはKubeadmApply前にSnapshotRefを要求する(t *testing.T) {
	plan, err := BuildPlan(PlanInput{
		Distribution:   DistributionKubeadm,
		OperationID:    "operation-1",
		CurrentVersion: "v1.34.0",
		TargetVersion:  "v1.35.0",
		UpdateClass:    UpdateClassKubernetesBinary,
		NodeRole:       NodeRoleControlPlane,
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}

	if err := ReadyForStep(plan, StepDistributionApplied); err == nil {
		t.Fatal("ReadyForStep(DistributionApplied) error = nil, want SnapshotRef required")
	}
	plan.SnapshotRef = "etcd-snapshot-1"
	if err := ReadyForStep(plan, StepDistributionApplied); err != nil {
		t.Fatalf("ReadyForStep(DistributionApplied) error = %v", err)
	}
}

func TestRecordPlanStepはPlanごとのStep順序を使う(t *testing.T) {
	plan, err := BuildPlan(PlanInput{
		Distribution:   DistributionKubeadm,
		OperationID:    "operation-1",
		CurrentVersion: "v1.34.0",
		TargetVersion:  "v1.35.0",
		UpdateClass:    UpdateClassKubernetesBinary,
		NodeRole:       NodeRoleWorker,
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	completed, _, err := RecordPlanStep(nil, StepPreflightCompleted, plan.Steps)
	if err != nil {
		t.Fatalf("RecordPlanStep(PreflightCompleted) error = %v", err)
	}
	completed, _, err = RecordPlanStep(completed, StepTargetSlotWritten, plan.Steps)
	if err != nil {
		t.Fatalf("RecordPlanStep(TargetSlotWritten) error = %v", err)
	}
	if slices.Contains(completed, StepSnapshotCreated) {
		t.Fatalf("completed = %v, want no SnapshotCreated for worker", completed)
	}
}
