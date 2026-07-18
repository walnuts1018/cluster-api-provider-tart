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

	domain "github.com/walnuts1018/cluster-api-provider-tart/domain/node/entity/nodelifecycleengine"
	sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"
	sharedworkflow "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/workflow"
)

type Command struct {
	Plan domain.Plan
	Step domain.Step
}

type Event interface{ isEvent() }

type StepRun struct{ Result StepResult }

func (StepRun) isEvent() {}

// WorkflowはNode Lifecycle Engine PlanのStepをDriverへdispatchする。
type Workflow struct {
	effects effectRunner
}

type Driver interface {
	PreflightRunner
	SnapshotCreator
	LifecycleApplier
	HealthObserver
}

// NewWorkflowはNode Lifecycle Engine Workflowを作る。
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
func (workflow *Workflow) Do(
	ctx context.Context,
	command Command,
) sharedresult.Result[Event, sharedworkflow.Failure] {
	if err := ensureRunnable(command.Plan, command.Step); err != nil {
		return sharedworkflow.Failed[Event](sharedworkflow.InvariantViolation{Detail: err.Error()})
	}
	result, err := workflow.effects.run(ctx, command.Plan, command.Step)
	if err != nil {
		return sharedworkflow.Failed[Event](sharedworkflow.DependencyFailure{Operation: "run node lifecycle step", Detail: err.Error()})
	}
	return sharedworkflow.Succeeded[Event](StepRun{Result: result})
}
