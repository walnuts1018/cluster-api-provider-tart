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

func TestDriverSetはPlanのLifecycleRuntimeでDriverを選ぶ(t *testing.T) {
	t.Parallel()

	kubeadm := &driverSetRecordingDriver{name: "kubeadm"}
	k0s := &driverSetRecordingDriver{name: "k0s"}
	set := NewDriverSet(map[domain.LifecycleRuntime]Driver{
		domain.LifecycleRuntimeKubeadm:     kubeadm,
		domain.LifecycleRuntimeUnsupported: &driverSetRecordingDriver{name: "unsupported"},
		domain.LifecycleRuntimeK0s:         k0s,
	})
	plan := domain.Plan{LifecycleRuntime: domain.LifecycleRuntimeK0s}

	if err := set.Apply(t.Context(), plan); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(kubeadm.applied) != 0 {
		t.Fatalf("kubeadm applied = %d, want 0", len(kubeadm.applied))
	}
	if len(k0s.applied) != 1 || k0s.applied[0].LifecycleRuntime != domain.LifecycleRuntimeK0s {
		t.Fatalf("k0s applied = %#v, want k0s plan", k0s.applied)
	}
}

func TestDriverSetは未登録LifecycleRuntimeを拒否する(t *testing.T) {
	t.Parallel()

	set := NewDriverSet(map[domain.LifecycleRuntime]Driver{
		domain.LifecycleRuntimeKubeadm:     nil,
		domain.LifecycleRuntimeUnsupported: nil,
		domain.LifecycleRuntimeK0s:         nil,
	})
	err := set.Preflight(t.Context(), domain.Plan{LifecycleRuntime: domain.LifecycleRuntime("unknown")})
	if err == nil {
		t.Fatal("Preflight() error = nil, want unregistered lifecycle runtime")
	}
}

type driverSetRecordingDriver struct {
	name    string
	applied []domain.Plan
}

func (driver *driverSetRecordingDriver) Preflight(context.Context, domain.Plan) error {
	return nil
}

func (driver *driverSetRecordingDriver) CreateSnapshot(context.Context, domain.Plan) (SnapshotResult, error) {
	return SnapshotResult{Ref: driver.name + "-snapshot", RestoreVerified: true}, nil
}

func (driver *driverSetRecordingDriver) Apply(_ context.Context, plan domain.Plan) error {
	driver.applied = append(driver.applied, plan)
	return nil
}

func (driver *driverSetRecordingDriver) ObserveHealth(context.Context, domain.Plan) (domain.HealthInput, error) {
	return domain.HealthInput{}, nil
}
