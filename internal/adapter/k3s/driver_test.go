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

package k3s

import (
	"context"
	"testing"

	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
)

func TestDriverは型付きK3sRuntimeへDispatchする(t *testing.T) {
	t.Parallel()

	runtime := &runtimeStub{snapshotRef: "k3s-snapshot-1"}
	driver := NewDriver(runtime)
	plan := domain.Plan{
		Distribution:  domain.DistributionK3s,
		OperationID:   "operation-1",
		TargetVersion: "v1.36.0",
		UpdateClass:   domain.UpdateClassKubernetesBinary,
		NodeRole:      domain.NodeRoleControlPlane,
	}

	if err := driver.Preflight(t.Context(), plan); err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	snapshot, err := driver.CreateSnapshot(t.Context(), plan)
	if err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}
	if snapshot.Ref != "k3s-snapshot-1" || !snapshot.RestoreVerified {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if err := driver.Apply(t.Context(), plan); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if runtime.preflightVersion != "v1.36.0" || runtime.applied.OperationID != "operation-1" {
		t.Fatalf("runtime = %#v", runtime)
	}
}

type runtimeStub struct {
	preflightVersion string
	snapshotRef      string
	applied          domain.Plan
}

func (stub *runtimeStub) Preflight(_ context.Context, targetVersion string) error {
	stub.preflightVersion = targetVersion
	return nil
}

func (stub *runtimeStub) SaveSnapshot(context.Context, string) (string, error) {
	return stub.snapshotRef, nil
}

func (stub *runtimeStub) VerifySnapshot(context.Context, string) error {
	return nil
}

func (stub *runtimeStub) Apply(_ context.Context, plan domain.Plan) error {
	stub.applied = plan
	return nil
}

func (stub *runtimeStub) ObserveHealth(context.Context, domain.Plan) (domain.HealthInput, error) {
	return domain.HealthInput{}, nil
}
