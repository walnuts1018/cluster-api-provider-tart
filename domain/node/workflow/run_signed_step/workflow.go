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

package nodelifecycle

import (
	"context"

	domain "github.com/walnuts1018/cluster-api-provider-tart/domain/node/entity/nodelifecycleengine"
	distribution "github.com/walnuts1018/cluster-api-provider-tart/domain/node/workflow/run_step"
	sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"
	sharedworkflow "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/workflow"
	agentprotocol "github.com/walnuts1018/cluster-api-provider-tart/dto/agent"
)

// StepRunnerは署名検証後のPlanだけを実際のLifecycle実装へ渡す境界である。
type StepRunner interface {
	Do(context.Context, distribution.Command) sharedresult.Result[distribution.Event, sharedworkflow.Failure]
}

type Command struct {
	Plan SignedPlan
	Step domain.Step
}

type Event interface{ isEvent() }

type SignedStepRun struct{ Result distribution.StepResult }

func (SignedStepRun) isEvent() {}

type Workflow struct {
	trustStore agentprotocol.TrustStore
	runner     StepRunner
}

func NewWorkflow(trustStore agentprotocol.TrustStore, runner StepRunner) *Workflow {
	return &Workflow{trustStore: trustStore, runner: runner}
}

// RunSignedStepは署名済みPlanだけを受理し、未署名または改ざん済みPlanを実行しない。
func (workflow *Workflow) Do(
	ctx context.Context,
	command Command,
) sharedresult.Result[Event, sharedworkflow.Failure] {
	if workflow.trustStore == nil {
		return sharedworkflow.Failed[Event](sharedworkflow.DependencyFailure{Operation: "run signed step", Detail: "lifecycle plan trust store is required"})
	}
	if workflow.runner == nil {
		return sharedworkflow.Failed[Event](sharedworkflow.DependencyFailure{Operation: "run signed step", Detail: "lifecycle step runner is required"})
	}
	validated, err := ValidatePlan(command.Plan.Plan)
	if err != nil {
		return sharedworkflow.Failed[Event](sharedworkflow.InvalidCommand{Detail: err.Error()})
	}
	if err := VerifySignature(validated, command.Plan.Signature, workflow.trustStore); err != nil {
		return sharedworkflow.Failed[Event](sharedworkflow.InvalidCommand{Detail: err.Error()})
	}
	plan, err := validated.DomainPlan()
	if err != nil {
		return sharedworkflow.Failed[Event](sharedworkflow.InvalidCommand{Detail: err.Error()})
	}
	if err := domain.ReadyForStep(plan, command.Step); err != nil {
		return sharedworkflow.Failed[Event](sharedworkflow.InvariantViolation{Detail: err.Error()})
	}
	outcome := workflow.runner.Do(ctx, distribution.Command{Plan: plan, Step: command.Step})
	event, present := outcome.Value().Value()
	if !present {
		failure, _ := outcome.FailureValue().Value()
		return sharedworkflow.Failed[Event](failure)
	}
	return sharedworkflow.Succeeded[Event](SignedStepRun{Result: event.(distribution.StepRun).Result})
}
