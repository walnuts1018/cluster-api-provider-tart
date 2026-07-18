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

package nodelifecycleengine

import (
	"context"
	"testing"

	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/nodelifecycleengine"
)

func TestWorkflowはLifecycleStepをDriverの型付き操作へDispatchする(t *testing.T) {
	driver := &recordingDriver{snapshot: SnapshotResult{Ref: "etcd-snapshot-1", RestoreVerified: true}}
	workflow := NewWorkflow(driver)
	plan := controlPlanePlan(t)

	for _, step := range []domain.Step{
		domain.StepPreflightCompleted,
		domain.StepSnapshotCreated,
		domain.StepDistributionApplied,
		domain.StepHealthVerified,
	} {
		if _, err := workflow.RunStep(t.Context(), plan, step); err != nil {
			t.Fatalf("RunStep(%q) error = %v", step, err)
		}
	}

	want := []domain.Step{
		domain.StepPreflightCompleted,
		domain.StepSnapshotCreated,
		domain.StepDistributionApplied,
		domain.StepHealthVerified,
	}
	if len(driver.calls) != len(want) {
		t.Fatalf("driver calls = %v, want %v", driver.calls, want)
	}
	for index, step := range want {
		if driver.calls[index] != step {
			t.Fatalf("driver calls = %v, want %v", driver.calls, want)
		}
	}
}

func TestWorkflowはRestoreTestに失敗したSnapshotを拒否する(t *testing.T) {
	driver := &recordingDriver{snapshot: SnapshotResult{Ref: "etcd-snapshot-1"}}
	workflow := NewWorkflow(driver)

	result, err := workflow.RunStep(t.Context(), controlPlanePlan(t), domain.StepSnapshotCreated)
	if err == nil {
		t.Fatalf("RunStep(SnapshotCreated) result=%#v, want restore test error", result)
	}
}

func TestWorkflowはSnapshotRefなしにKubeadmApplyを実行しない(t *testing.T) {
	driver := &recordingDriver{snapshot: SnapshotResult{Ref: "etcd-snapshot-1", RestoreVerified: true}}
	workflow := NewWorkflow(driver)
	plan := controlPlanePlanWithoutSnapshot(t)

	if _, err := workflow.RunStep(t.Context(), plan, domain.StepDistributionApplied); err == nil {
		t.Fatal("RunStep(DistributionApplied) error = nil, want SnapshotRef required")
	}
	if len(driver.calls) != 0 {
		t.Fatalf("driver calls = %v, want no calls", driver.calls)
	}
}

func controlPlanePlan(t *testing.T) domain.Plan {
	t.Helper()
	plan, err := domain.BuildPlan(domain.PlanInput{
		LifecycleRuntime: domain.LifecycleRuntimeKubeadm,
		OperationID:      "operation-1",
		CurrentVersion:   "v1.35.0",
		TargetVersion:    "v1.36.0",
		UpdateClass:      domain.UpdateClassKubernetesBinary,
		NodeRole:         domain.NodeRoleControlPlane,
		SnapshotRef:      "etcd-snapshot-1",
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	return plan
}

func controlPlanePlanWithoutSnapshot(t *testing.T) domain.Plan {
	t.Helper()
	plan, err := domain.BuildPlan(domain.PlanInput{
		LifecycleRuntime: domain.LifecycleRuntimeKubeadm,
		OperationID:      "operation-1",
		CurrentVersion:   "v1.35.0",
		TargetVersion:    "v1.36.0",
		UpdateClass:      domain.UpdateClassKubernetesBinary,
		NodeRole:         domain.NodeRoleControlPlane,
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	return plan
}

type recordingDriver struct {
	calls    []domain.Step
	snapshot SnapshotResult
}

func (driver *recordingDriver) Preflight(context.Context, domain.Plan) error {
	driver.calls = append(driver.calls, domain.StepPreflightCompleted)
	return nil
}

func (driver *recordingDriver) CreateSnapshot(context.Context, domain.Plan) (SnapshotResult, error) {
	driver.calls = append(driver.calls, domain.StepSnapshotCreated)
	return driver.snapshot, nil
}

func (driver *recordingDriver) Apply(context.Context, domain.Plan) error {
	driver.calls = append(driver.calls, domain.StepDistributionApplied)
	return nil
}

func (driver *recordingDriver) Verify(context.Context, domain.Plan) error {
	driver.calls = append(driver.calls, domain.StepHealthVerified)
	return nil
}

func (driver *recordingDriver) ObserveHealth(context.Context, domain.Plan) (domain.HealthInput, error) {
	driver.calls = append(driver.calls, domain.StepHealthVerified)
	return domain.HealthInput{
		NodeReady:       true,
		NodeVersion:     "v1.36.0",
		StaticPodsReady: true,
		EtcdQuorum:      true,
		APIHealthy:      true,
	}, nil
}
