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

	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
)

// WorkflowはDistribution Lifecycle PlanのStepをDriverへdispatchする。
type Workflow struct {
	effects effectRunner
}

type Driver interface {
	PreflightRunner
	SnapshotCreator
	LifecycleApplier
	HealthObserver
}

// NewWorkflowはDistribution Lifecycle Workflowを作る。
func NewWorkflow(driver Driver) *Workflow {
	return &Workflow{
		effects: effectRunner{
			preflight: driver,
			snapshot:  driver,
			apply:     driver,
			health:    driver,
		},
	}
}

// RunStepは任意commandではなく、Plan内の既知StepだけをDriverへdispatchする。
func (workflow *Workflow) RunStep(
	ctx context.Context,
	plan domain.Plan,
	step domain.Step,
) (StepResult, error) {
	if err := ensureRunnable(plan, step); err != nil {
		return StepResult{}, err
	}
	return workflow.effects.run(ctx, plan, step)
}
