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
	"crypto/ed25519"
	"fmt"
	"time"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	cleaningevent "github.com/walnuts1018/cluster-api-provider-tart/internal/application/cleaning/event"
	cleaninghandler "github.com/walnuts1018/cluster-api-provider-tart/internal/application/cleaning/handler"
	cleaningmodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/cleaning/model"
	cleaningport "github.com/walnuts1018/cluster-api-provider-tart/internal/application/cleaning/port"
	cleaningstep "github.com/walnuts1018/cluster-api-provider-tart/internal/application/cleaning/step"
)

type HostPhaseService = cleaningport.HostPhaseService
type OperationService = cleaningport.OperationService
type PlanWriter = cleaningport.PlanWriter

type PlanSigner struct {
	KeyID      string
	PrivateKey ed25519.PrivateKey
}

type Step = cleaningstep.Step
type StepMarkHostCleaning = cleaningstep.MarkHostCleaning
type StepStartOperation = cleaningstep.StartOperation
type StepPersistPlan = cleaningstep.PersistPlan

type Event = cleaningevent.Event
type EventOperationStarted = cleaningevent.OperationStarted
type EventPlanPersisted = cleaningevent.PlanPersisted
type StartResult = cleaningmodel.StartResult

type Workflow struct {
	commands *cleaninghandler.CommandHandler
	signer   PlanSigner
	now      func() time.Time
}

func NewWorkflow(
	hostPhase HostPhaseService,
	operations OperationService,
	plans PlanWriter,
	signer PlanSigner,
) *Workflow {
	steps := cleaningstep.NewExecutor(hostPhase, operations, plans)
	return &Workflow{
		commands: cleaninghandler.NewCommandHandler(steps),
		signer:   signer,
		now:      time.Now,
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
	if err := workflow.commands.MarkHostCleaning(ctx, host, machine.Spec.DeletionPolicy); err != nil {
		return cleaningmodel.StartResult{}, err
	}
	draft, err := BuildOperationDraft(machine, host, "", workflow.now())
	if err != nil {
		return cleaningmodel.StartResult{}, err
	}
	candidatePlan, err := buildSignedCleaningPlanStep(host, machine.Spec.DeletionPolicy, draft, workflow.signer)
	if err != nil {
		return cleaningmodel.StartResult{}, err
	}
	draft.Spec.PlanDigest = candidatePlan.Digest.String()

	started, err := workflow.commands.StartOperation(ctx, StepStartOperation{Operation: draft})
	if err != nil {
		return cleaningmodel.StartResult{}, err
	}
	persistedPlan, err := buildSignedCleaningPlanStep(host, machine.Spec.DeletionPolicy, started, workflow.signer)
	if err != nil {
		return cleaningmodel.StartResult{}, err
	}
	if persistedPlan.Digest.String() != started.Spec.PlanDigest {
		return cleaningmodel.StartResult{}, fmt.Errorf("stored Cleaning Operation Plan digest does not match regenerated Plan")
	}
	if err := workflow.commands.PersistPlan(ctx, StepPersistPlan{
		Operation: started,
		Plan:      persistedPlan.Plan,
		Signature: persistedPlan.Signature,
	}); err != nil {
		return cleaningmodel.StartResult{}, err
	}
	return cleaningmodel.StartResult{
		Operation: started,
		Events: []cleaningevent.Event{
			EventOperationStarted{OperationID: started.Spec.OperationID},
			EventPlanPersisted{OperationID: started.Spec.OperationID},
		},
	}, nil
}
