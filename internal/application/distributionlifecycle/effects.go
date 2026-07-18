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

type effectRunner struct {
	preflight PreflightRunner
	snapshot  SnapshotCreator
	apply     LifecycleApplier
	health    HealthObserver
}

func (runner effectRunner) run(
	ctx context.Context,
	plan domain.Plan,
	step domain.Step,
) (StepResult, error) {
	switch step {
	case domain.StepPreflightCompleted:
		if runner.preflight == nil {
			return StepResult{}, fmt.Errorf("preflight runner is required")
		}
		return StepResult{}, runner.preflight.Preflight(ctx, plan)
	case domain.StepSnapshotCreated:
		if runner.snapshot == nil {
			return StepResult{}, fmt.Errorf("snapshot creator is required")
		}
		snapshot, err := runner.snapshot.CreateSnapshot(ctx, plan)
		if err != nil {
			return StepResult{}, err
		}
		return presentSnapshot(snapshot)
	case domain.StepDistributionApplied:
		if runner.apply == nil {
			return StepResult{}, fmt.Errorf("lifecycle applier is required")
		}
		return StepResult{}, runner.apply.Apply(ctx, plan)
	case domain.StepHealthVerified:
		if runner.health == nil {
			return StepResult{}, fmt.Errorf("health observer is required")
		}
		observation, err := runner.health.ObserveHealth(ctx, plan)
		if err != nil {
			return StepResult{}, err
		}
		observation.TargetVersion = plan.TargetVersion
		observation.NodeRole = plan.NodeRole
		return presentHealth(domain.DecideHealth(observation))
	case domain.StepTargetSlotWritten, domain.StepTargetSlotBooted, domain.StepCommitted:
		return StepResult{}, fmt.Errorf("lifecycle step %q is handled by the OS update controller", step)
	default:
		return StepResult{}, fmt.Errorf("unknown lifecycle step %q", step)
	}
}
