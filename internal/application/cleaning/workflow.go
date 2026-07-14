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

package cleaning

import (
	"context"
	"fmt"
	"time"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	cleaningevent "github.com/walnuts1018/cluster-api-provider-tart/internal/application/cleaning/event"
	cleaningmodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/cleaning/model"
	cleaningport "github.com/walnuts1018/cluster-api-provider-tart/internal/application/cleaning/port"
	cleaningstep "github.com/walnuts1018/cluster-api-provider-tart/internal/application/cleaning/step"
)

type HostPhaseService = cleaningport.HostPhaseService
type OperationService = cleaningport.OperationService
type PlanWriter = cleaningport.PlanWriter

type PlanSigner = cleaningstep.PlanSigner

type Step = cleaningstep.Step
type StepMarkHostCleaning = cleaningstep.MarkHostCleaning
type StepStartOperation = cleaningstep.StartOperation
type StepPersistPlan = cleaningstep.PersistPlan

type Event = cleaningevent.Event
type EventOperationStarted = cleaningevent.OperationStarted
type EventPlanPersisted = cleaningevent.PlanPersisted
type StartResult = cleaningmodel.StartResult

type Workflow struct {
	steps *cleaningstep.Executor
	now   func() time.Time
}

func NewWorkflow(
	hostPhase HostPhaseService,
	operations OperationService,
	plans PlanWriter,
	signer PlanSigner,
) *Workflow {
	steps := cleaningstep.NewExecutor(hostPhase, operations, plans, signer)
	return &Workflow{
		steps: steps,
		now:   time.Now,
	}
}

func (workflow *Workflow) StartCleaning(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
) (*infrastructurev1beta1.TartHostOperation, error) {
	result, err := workflow.startCleaning(ctx, machine, host)
	if err != nil {
		return nil, err
	}
	return result.Operation, nil
}

func (workflow *Workflow) startCleaning(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
) (cleaningmodel.StartResult, error) {
	if err := workflow.markHostCleaning(ctx, host, machine.Spec.DeletionPolicy); err != nil {
		return cleaningmodel.StartResult{}, err
	}
	draft, err := cleaningstep.BuildOperationDraft(machine, host, "", workflow.now())
	if err != nil {
		return cleaningmodel.StartResult{}, err
	}
	candidatePlan, err := workflow.steps.BuildSignedCleaningPlan(host, machine.Spec.DeletionPolicy, draft)
	if err != nil {
		return cleaningmodel.StartResult{}, err
	}
	draft.Spec.PlanDigest = candidatePlan.Digest.String()

	started, err := workflow.startOperation(ctx, cleaningstep.StartOperation{Operation: draft})
	if err != nil {
		return cleaningmodel.StartResult{}, err
	}
	persistedPlan, err := workflow.steps.BuildSignedCleaningPlan(host, machine.Spec.DeletionPolicy, started)
	if err != nil {
		return cleaningmodel.StartResult{}, err
	}
	if persistedPlan.Digest.String() != started.Spec.PlanDigest {
		return cleaningmodel.StartResult{}, fmt.Errorf("stored Cleaning Operation Plan digest does not match regenerated Plan")
	}
	if err := workflow.persistPlan(ctx, cleaningstep.PersistPlan{
		Operation: started,
		Plan:      persistedPlan.Plan,
		Signature: persistedPlan.Signature,
	}); err != nil {
		return cleaningmodel.StartResult{}, err
	}
	return cleaningmodel.StartResult{
		Operation: started,
		Events: []cleaningevent.Event{
			cleaningevent.OperationStarted{OperationID: started.Spec.OperationID},
			cleaningevent.PlanPersisted{OperationID: started.Spec.OperationID},
		},
	}, nil
}

func (workflow *Workflow) markHostCleaning(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	deletionPolicy infrastructurev1beta1.DeletionPolicy,
) error {
	if err := workflow.steps.MarkHostCleaning(ctx, host, deletionPolicy); err != nil {
		return fmt.Errorf("mark TartHost cleaning: %w", err)
	}
	return nil
}

func (workflow *Workflow) startOperation(
	ctx context.Context,
	command cleaningstep.StartOperation,
) (*infrastructurev1beta1.TartHostOperation, error) {
	operation, err := workflow.steps.StartOperation(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("start Cleaning operation: %w", err)
	}
	return operation, nil
}

func (workflow *Workflow) persistPlan(ctx context.Context, command cleaningstep.PersistPlan) error {
	if err := workflow.steps.PersistPlan(ctx, command); err != nil {
		return fmt.Errorf("persist Cleaning Plan: %w", err)
	}
	return nil
}
