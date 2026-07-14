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
	"fmt"

	application "github.com/walnuts1018/cluster-api-provider-tart/internal/application/distributionlifecycle"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
)

// RuntimeはNode Lifecycle Service内でkubeadm/etcd/health観測を型付き操作として実行する境界である。
type Runtime interface {
	UpgradePlan(context.Context, string) error
	SaveEtcdSnapshot(context.Context, string) (string, error)
	VerifyEtcdSnapshot(context.Context, string) error
	UpgradeApply(context.Context, string) error
	UpgradeNode(context.Context, string) error
	ObserveHealth(context.Context, domain.Plan) (domain.HealthInput, error)
}

// DriverはDistributionLifecycleDriver Portのkubeadm実装である。
type Driver struct {
	runtime Runtime
}

func NewDriver(runtime Runtime) *Driver {
	return &Driver{runtime: runtime}
}

func (driver *Driver) Preflight(ctx context.Context, plan domain.Plan) error {
	if driver.runtime == nil {
		return fmt.Errorf("kubeadm runtime is required")
	}
	return driver.runtime.UpgradePlan(ctx, plan.TargetVersion)
}

func (driver *Driver) CreateSnapshot(ctx context.Context, plan domain.Plan) (application.SnapshotResult, error) {
	if driver.runtime == nil {
		return application.SnapshotResult{}, fmt.Errorf("kubeadm runtime is required")
	}
	ref, err := driver.runtime.SaveEtcdSnapshot(ctx, plan.OperationID)
	if err != nil {
		return application.SnapshotResult{}, err
	}
	if ref == "" {
		return application.SnapshotResult{}, fmt.Errorf("etcd snapshot reference is required")
	}
	if err := driver.runtime.VerifyEtcdSnapshot(ctx, ref); err != nil {
		return application.SnapshotResult{}, err
	}
	return application.SnapshotResult{Ref: ref, RestoreVerified: true}, nil
}

func (driver *Driver) Apply(ctx context.Context, plan domain.Plan) error {
	if driver.runtime == nil {
		return fmt.Errorf("kubeadm runtime is required")
	}
	switch plan.NodeRole {
	case domain.NodeRoleControlPlane:
		return driver.runtime.UpgradeApply(ctx, plan.TargetVersion)
	case domain.NodeRoleWorker:
		return driver.runtime.UpgradeNode(ctx, plan.TargetVersion)
	default:
		return fmt.Errorf("unsupported node role %q", plan.NodeRole)
	}
}

func (driver *Driver) ObserveHealth(
	ctx context.Context,
	plan domain.Plan,
) (domain.HealthInput, error) {
	if driver.runtime == nil {
		return domain.HealthInput{}, fmt.Errorf("kubeadm runtime is required")
	}
	health, err := driver.runtime.ObserveHealth(ctx, plan)
	if err != nil {
		return domain.HealthInput{}, err
	}
	return health, nil
}

func (driver *Driver) Verify(ctx context.Context, plan domain.Plan) error {
	health, err := driver.ObserveHealth(ctx, plan)
	if err != nil {
		return err
	}
	health.TargetVersion = plan.TargetVersion
	health.NodeRole = plan.NodeRole
	switch domain.DecideHealth(health).(type) {
	case domain.HealthGateSatisfied:
		return nil
	case domain.HealthGateBlocked:
		return fmt.Errorf("distribution health gate failed")
	default:
		return fmt.Errorf("unsupported health decision")
	}
}
