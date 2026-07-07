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
	"context"
	"fmt"

	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
)

// DistributionLifecycleDriverはkubeadm等のdistribution固有更新を型付きStepだけで実行するPortである。
type DistributionLifecycleDriver interface {
	Preflight(context.Context, domain.Plan) error
	CreateSnapshot(context.Context, domain.Plan) (SnapshotResult, error)
	Apply(context.Context, domain.Plan) error
	Verify(context.Context, domain.Plan) error
}

// SnapshotResultはSnapshot作成とrestore testの結果である。
type SnapshotResult struct {
	Ref             string
	RestoreVerified bool
}

// StepResultはLifecycle Step実行結果のうちOperation Statusへ反映する情報である。
type StepResult struct {
	SnapshotRef string
}

// ServiceはDistribution Lifecycle PlanのStepをDriverへdispatchする。
type Service struct {
	driver DistributionLifecycleDriver
}

// NewServiceはDistribution Lifecycle Serviceを作る。
func NewService(driver DistributionLifecycleDriver) *Service {
	return &Service{driver: driver}
}

// RunStepは任意commandではなく、Plan内の既知StepだけをDriverへdispatchする。
func (service *Service) RunStep(
	ctx context.Context,
	plan domain.Plan,
	step domain.Step,
) (StepResult, error) {
	if service.driver == nil {
		return StepResult{}, fmt.Errorf("DistributionLifecycleDriver is required")
	}
	if !stepInPlan(step, plan.Steps) {
		return StepResult{}, fmt.Errorf("lifecycle step %q is not part of this plan", step)
	}

	switch step {
	case domain.StepPreflightCompleted:
		return StepResult{}, service.driver.Preflight(ctx, plan)
	case domain.StepSnapshotCreated:
		snapshot, err := service.driver.CreateSnapshot(ctx, plan)
		if err != nil {
			return StepResult{}, err
		}
		if snapshot.Ref == "" {
			return StepResult{}, fmt.Errorf("snapshot reference is required")
		}
		if !snapshot.RestoreVerified {
			return StepResult{}, fmt.Errorf("snapshot restore test must pass before using SnapshotRef")
		}
		return StepResult{SnapshotRef: snapshot.Ref}, nil
	case domain.StepKubeadmApplied:
		return StepResult{}, service.driver.Apply(ctx, plan)
	case domain.StepHealthVerified:
		return StepResult{}, service.driver.Verify(ctx, plan)
	case domain.StepTargetSlotWritten, domain.StepTargetSlotBooted, domain.StepCommitted:
		return StepResult{}, fmt.Errorf("lifecycle step %q is handled by the OS update controller", step)
	default:
		return StepResult{}, fmt.Errorf("unknown lifecycle step %q", step)
	}
}
