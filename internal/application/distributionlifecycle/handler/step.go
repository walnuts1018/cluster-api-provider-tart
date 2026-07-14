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

package handler

import (
	"context"
	"fmt"

	distributionlifecyclemodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/distributionlifecycle/model"
	distributionlifecycleport "github.com/walnuts1018/cluster-api-provider-tart/internal/application/distributionlifecycle/port"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
)

type StepHandler struct {
	driver distributionlifecycleport.DistributionLifecycleDriver
}

func NewStepHandler(driver distributionlifecycleport.DistributionLifecycleDriver) *StepHandler {
	return &StepHandler{driver: driver}
}

func (handler *StepHandler) Handle(
	ctx context.Context,
	plan domain.Plan,
	step domain.Step,
) (distributionlifecyclemodel.StepResult, error) {
	if handler.driver == nil {
		return distributionlifecyclemodel.StepResult{}, fmt.Errorf("DistributionLifecycleDriver is required")
	}
	switch step {
	case domain.StepPreflightCompleted:
		return distributionlifecyclemodel.StepResult{}, handler.driver.Preflight(ctx, plan)
	case domain.StepSnapshotCreated:
		snapshot, err := handler.driver.CreateSnapshot(ctx, plan)
		if err != nil {
			return distributionlifecyclemodel.StepResult{}, err
		}
		if snapshot.Ref == "" {
			return distributionlifecyclemodel.StepResult{}, fmt.Errorf("snapshot reference is required")
		}
		if !snapshot.RestoreVerified {
			return distributionlifecyclemodel.StepResult{}, fmt.Errorf("snapshot restore test must pass before using SnapshotRef")
		}
		return distributionlifecyclemodel.StepResult{SnapshotRef: snapshot.Ref}, nil
	case domain.StepKubeadmApplied:
		return distributionlifecyclemodel.StepResult{}, handler.driver.Apply(ctx, plan)
	case domain.StepHealthVerified:
		return distributionlifecyclemodel.StepResult{}, handler.driver.Verify(ctx, plan)
	case domain.StepTargetSlotWritten, domain.StepTargetSlotBooted, domain.StepCommitted:
		return distributionlifecyclemodel.StepResult{}, fmt.Errorf("lifecycle step %q is handled by the OS update controller", step)
	default:
		return distributionlifecyclemodel.StepResult{}, fmt.Errorf("unknown lifecycle step %q", step)
	}
}
