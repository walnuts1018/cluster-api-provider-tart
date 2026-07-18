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

package k0s

import (
	"context"
	"testing"

	application "github.com/walnuts1018/cluster-api-provider-tart/internal/application/nodelifecycleengine"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/nodelifecycleengine"
)

func TestDriverはControlPlaneとWorkerのApplyを型付きRuntimeへDispatchする(t *testing.T) {
	t.Parallel()

	runtime := &recordingRuntime{}
	driver := NewDriver(runtime)

	if err := driver.Apply(t.Context(), plan(domain.NodeRoleControlPlane)); err != nil {
		t.Fatalf("Apply(control plane) error = %v", err)
	}
	if err := driver.Apply(t.Context(), plan(domain.NodeRoleWorker)); err != nil {
		t.Fatalf("Apply(worker) error = %v", err)
	}
	if !equalCalls(runtime.calls, []string{"UpgradeController:v1.36.0", "UpgradeWorker:v1.36.0"}) {
		t.Fatalf("calls = %#v", runtime.calls)
	}
}

func TestDriverはSnapshot作成後にRestoreTestを実行する(t *testing.T) {
	t.Parallel()

	runtime := &recordingRuntime{snapshotRef: "k0s-snapshot-1"}
	driver := NewDriver(runtime)

	got, err := driver.CreateSnapshot(t.Context(), plan(domain.NodeRoleControlPlane))
	if err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}
	if got != (application.SnapshotResult{Ref: "k0s-snapshot-1", RestoreVerified: true}) {
		t.Fatalf("snapshot = %#v", got)
	}
	if !equalCalls(runtime.calls, []string{"SaveSnapshot:operation-1", "VerifySnapshot:k0s-snapshot-1"}) {
		t.Fatalf("calls = %#v", runtime.calls)
	}
}

func plan(role domain.NodeRole) domain.Plan {
	return domain.Plan{
		LifecycleRuntime: domain.LifecycleRuntimeK0s,
		OperationID:      "operation-1",
		TargetVersion:    "v1.36.0",
		UpdateClass:      domain.UpdateClassKubernetesBinary,
		NodeRole:         role,
		CurrentVersion:   "v1.35.0",
	}
}

type recordingRuntime struct {
	calls       []string
	snapshotRef string
}

func (runtime *recordingRuntime) Preflight(_ context.Context, targetVersion string) error {
	runtime.calls = append(runtime.calls, "Preflight:"+targetVersion)
	return nil
}

func (runtime *recordingRuntime) SaveSnapshot(_ context.Context, operationID string) (string, error) {
	runtime.calls = append(runtime.calls, "SaveSnapshot:"+operationID)
	return runtime.snapshotRef, nil
}

func (runtime *recordingRuntime) VerifySnapshot(_ context.Context, snapshotRef string) error {
	runtime.calls = append(runtime.calls, "VerifySnapshot:"+snapshotRef)
	return nil
}

func (runtime *recordingRuntime) UpgradeController(_ context.Context, targetVersion string) error {
	runtime.calls = append(runtime.calls, "UpgradeController:"+targetVersion)
	return nil
}

func (runtime *recordingRuntime) UpgradeWorker(_ context.Context, targetVersion string) error {
	runtime.calls = append(runtime.calls, "UpgradeWorker:"+targetVersion)
	return nil
}

func (runtime *recordingRuntime) ObserveHealth(context.Context, domain.Plan) (domain.HealthInput, error) {
	return domain.HealthInput{}, nil
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
