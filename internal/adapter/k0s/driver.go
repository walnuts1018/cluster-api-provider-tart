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
	"fmt"

	application "github.com/walnuts1018/cluster-api-provider-tart/internal/application/nodelifecycleengine"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/nodelifecycleengine"
)

// RuntimeはNode Lifecycle Service内でk0sの更新とhealth観測を型付き操作として実行する境界である。
type Runtime interface {
	Preflight(context.Context, string) error
	SaveSnapshot(context.Context, string) (string, error)
	VerifySnapshot(context.Context, string) error
	UpgradeController(context.Context, string) error
	UpgradeWorker(context.Context, string) error
	ObserveHealth(context.Context, domain.Plan) (domain.HealthInput, error)
}

// DriverはNodeLifecycleEngine Portのk0s実装である。
type Driver struct {
	runtime Runtime
}

func NewDriver(runtime Runtime) *Driver {
	return &Driver{runtime: runtime}
}

func (driver *Driver) Preflight(ctx context.Context, plan domain.Plan) error {
	if driver.runtime == nil {
		return fmt.Errorf("k0s runtime is required")
	}
	return driver.runtime.Preflight(ctx, plan.TargetVersion)
}

func (driver *Driver) CreateSnapshot(ctx context.Context, plan domain.Plan) (application.SnapshotResult, error) {
	if driver.runtime == nil {
		return application.SnapshotResult{}, fmt.Errorf("k0s runtime is required")
	}
	ref, err := driver.runtime.SaveSnapshot(ctx, plan.OperationID)
	if err != nil {
		return application.SnapshotResult{}, err
	}
	if ref == "" {
		return application.SnapshotResult{}, fmt.Errorf("k0s snapshot reference is required")
	}
	if err := driver.runtime.VerifySnapshot(ctx, ref); err != nil {
		return application.SnapshotResult{}, err
	}
	return application.SnapshotResult{Ref: ref, RestoreVerified: true}, nil
}

func (driver *Driver) Apply(ctx context.Context, plan domain.Plan) error {
	if driver.runtime == nil {
		return fmt.Errorf("k0s runtime is required")
	}
	switch plan.NodeRole {
	case domain.NodeRoleControlPlane:
		return driver.runtime.UpgradeController(ctx, plan.TargetVersion)
	case domain.NodeRoleWorker:
		return driver.runtime.UpgradeWorker(ctx, plan.TargetVersion)
	default:
		return fmt.Errorf("unsupported node role %q", plan.NodeRole)
	}
}

func (driver *Driver) ObserveHealth(ctx context.Context, plan domain.Plan) (domain.HealthInput, error) {
	if driver.runtime == nil {
		return domain.HealthInput{}, fmt.Errorf("k0s runtime is required")
	}
	return driver.runtime.ObserveHealth(ctx, plan)
}
