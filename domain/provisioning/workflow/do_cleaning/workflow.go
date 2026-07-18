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
	cleaningevent "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/event/cleaning"
	cleaningstep "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/step/cleaning"
	sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"
	sharedworkflow "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/workflow"
)

type PlanSigner = cleaningstep.PlanSigner

type DomainEvent = cleaningevent.Event
type EventOperationStarted = cleaningevent.OperationStarted
type EventPlanPersisted = cleaningevent.PlanPersisted

type Command struct {
	Machine *infrastructurev1beta1.TartMachine
	Host    *infrastructurev1beta1.TartHost
}

type Event struct {
	Operation *infrastructurev1beta1.TartHostOperation
}

type Workflow struct {
	hostPhase  HostPhaseService
	operations OperationService
	plans      PlanWriter
	signer     PlanSigner
	now        func() time.Time
}

func NewWorkflow(
	hostPhase HostPhaseService,
	operations OperationService,
	plans PlanWriter,
	signer PlanSigner,
) *Workflow {
	return &Workflow{
		hostPhase: hostPhase, operations: operations, plans: plans, signer: signer, now: time.Now,
	}
}

func (workflow *Workflow) Do(
	ctx context.Context,
	command Command,
) sharedresult.Result[Event, sharedworkflow.Failure] {
	result, err := workflow.startCleaning(ctx, command.Machine, command.Host)
	if err != nil {
		return sharedworkflow.Failed[Event](sharedworkflow.DependencyFailure{Operation: "start cleaning", Detail: err.Error()})
	}
	return sharedworkflow.Succeeded[Event](Event{Operation: result.Operation})
}

func (workflow *Workflow) startCleaning(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
) (StartResult, error) {
	if err := workflow.markHostCleaning(ctx, host, machine.Spec.DeletionPolicy); err != nil {
		return StartResult{}, err
	}
	draft, err := cleaningstep.BuildOperationDraft(machine, host, "", workflow.now())
	if err != nil {
		return StartResult{}, err
	}
	candidatePlan, err := cleaningstep.BuildSignedCleaningPlan(host, machine.Spec.DeletionPolicy, draft, workflow.signer)
	if err != nil {
		return StartResult{}, err
	}
	draft.Spec.PlanDigest = candidatePlan.Digest.String()

	started, err := workflow.startOperation(ctx, draft)
	if err != nil {
		return StartResult{}, err
	}
	persistedPlan, err := cleaningstep.BuildSignedCleaningPlan(host, machine.Spec.DeletionPolicy, started, workflow.signer)
	if err != nil {
		return StartResult{}, err
	}
	if persistedPlan.Digest.String() != started.Spec.PlanDigest {
		return StartResult{}, fmt.Errorf("stored Cleaning Operation Plan digest does not match regenerated Plan")
	}
	if err := workflow.persistPlan(ctx, started, persistedPlan); err != nil {
		return StartResult{}, err
	}
	return StartResult{
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
	if err := workflow.hostPhase.MarkHostCleaningForDeletion(ctx, host, deletionPolicy); err != nil {
		return fmt.Errorf("mark TartHost cleaning: %w", err)
	}
	return nil
}

func (workflow *Workflow) startOperation(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
) (*infrastructurev1beta1.TartHostOperation, error) {
	operation, err := workflow.operations.Start(ctx, operation)
	if err != nil {
		return nil, fmt.Errorf("start Cleaning operation: %w", err)
	}
	return operation, nil
}

func (workflow *Workflow) persistPlan(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	plan cleaningstep.SignedCleaningPlan,
) error {
	if err := workflow.plans.Write(ctx, operation, plan.Plan, plan.Signature); err != nil {
		return fmt.Errorf("persist Cleaning Plan: %w", err)
	}
	return nil
}
