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

package kubeadm

import (
	"context"
	"testing"

	application "github.com/walnuts1018/cluster-api-provider-tart/internal/application/distributionlifecycle"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
)

func TestDriverはControlPlaneApplyでkubeadmUpgradeApplyを呼ぶ(t *testing.T) {
	runtime := &recordingRuntime{
		health: domain.HealthInput{
			NodeReady:       true,
			NodeVersion:     "v1.36.0",
			TargetVersion:   "v1.36.0",
			StaticPodsReady: true,
			EtcdQuorum:      true,
			APIHealthy:      true,
			NodeRole:        domain.NodeRoleControlPlane,
		},
	}
	driver := NewDriver(runtime)
	plan := controlPlanePlan(t)

	if err := driver.Apply(t.Context(), plan); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !equalCalls(runtime.calls, []string{"UpgradeApply:v1.36.0"}) {
		t.Fatalf("calls = %#v", runtime.calls)
	}
}

func TestDriverはWorkerApplyでkubeadmUpgradeNodeを呼ぶ(t *testing.T) {
	runtime := &recordingRuntime{}
	driver := NewDriver(runtime)
	plan := workerPlan(t)

	if err := driver.Apply(t.Context(), plan); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !equalCalls(runtime.calls, []string{"UpgradeNode:v1.36.0"}) {
		t.Fatalf("calls = %#v", runtime.calls)
	}
}

func TestDriverはSnapshot作成後にRestoreTestを実行する(t *testing.T) {
	runtime := &recordingRuntime{snapshotRef: "etcd-snapshot-1"}
	driver := NewDriver(runtime)

	got, err := driver.CreateSnapshot(t.Context(), controlPlanePlan(t))
	if err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}
	if got != (application.SnapshotResult{Ref: "etcd-snapshot-1", RestoreVerified: true}) {
		t.Fatalf("snapshot = %#v", got)
	}
	if !equalCalls(runtime.calls, []string{"SaveEtcdSnapshot:operation-1", "VerifyEtcdSnapshot:etcd-snapshot-1"}) {
		t.Fatalf("calls = %#v", runtime.calls)
	}
}

func TestDriverはHealthGate失敗時にVerifyを失敗させる(t *testing.T) {
	runtime := &recordingRuntime{
		health: domain.HealthInput{
			NodeReady:     true,
			NodeVersion:   "v1.35.0",
			TargetVersion: "v1.36.0",
			NodeRole:      domain.NodeRoleWorker,
		},
	}
	driver := NewDriver(runtime)

	if err := driver.Verify(t.Context(), workerPlan(t)); err == nil {
		t.Fatal("Verify() error = nil, want version mismatch")
	}
}

func controlPlanePlan(t *testing.T) domain.Plan {
	t.Helper()
	plan, err := domain.BuildPlan(domain.PlanInput{
		Distribution:   domain.DistributionKubeadm,
		OperationID:    "operation-1",
		CurrentVersion: "v1.35.0",
		TargetVersion:  "v1.36.0",
		UpdateClass:    domain.UpdateClassKubernetesBinary,
		NodeRole:       domain.NodeRoleControlPlane,
		SnapshotRef:    "etcd-snapshot-1",
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	return plan
}

func workerPlan(t *testing.T) domain.Plan {
	t.Helper()
	plan, err := domain.BuildPlan(domain.PlanInput{
		Distribution:   domain.DistributionKubeadm,
		OperationID:    "operation-1",
		CurrentVersion: "v1.35.0",
		TargetVersion:  "v1.36.0",
		UpdateClass:    domain.UpdateClassKubernetesBinary,
		NodeRole:       domain.NodeRoleWorker,
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	return plan
}

type recordingRuntime struct {
	calls       []string
	snapshotRef string
	health      domain.HealthInput
}

func (runtime *recordingRuntime) UpgradePlan(_ context.Context, targetVersion string) error {
	runtime.calls = append(runtime.calls, "UpgradePlan:"+targetVersion)
	return nil
}

func (runtime *recordingRuntime) SaveEtcdSnapshot(_ context.Context, operationID string) (string, error) {
	runtime.calls = append(runtime.calls, "SaveEtcdSnapshot:"+operationID)
	return runtime.snapshotRef, nil
}

func (runtime *recordingRuntime) VerifyEtcdSnapshot(_ context.Context, snapshotRef string) error {
	runtime.calls = append(runtime.calls, "VerifyEtcdSnapshot:"+snapshotRef)
	return nil
}

func (runtime *recordingRuntime) UpgradeApply(_ context.Context, targetVersion string) error {
	runtime.calls = append(runtime.calls, "UpgradeApply:"+targetVersion)
	return nil
}

func (runtime *recordingRuntime) UpgradeNode(_ context.Context, targetVersion string) error {
	runtime.calls = append(runtime.calls, "UpgradeNode:"+targetVersion)
	return nil
}

func (runtime *recordingRuntime) ObserveHealth(context.Context, domain.Plan) (domain.HealthInput, error) {
	return runtime.health, nil
}

func equalCalls(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
