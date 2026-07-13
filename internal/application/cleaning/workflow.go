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
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

type PlanWriter interface {
	Write(
		context.Context,
		*infrastructurev1beta1.TartHostOperation,
		agentprotocol.ValidatedPlan,
		agentprotocol.Signature,
	) error
}

type PlanSigner struct {
	KeyID      string
	PrivateKey ed25519.PrivateKey
}

type Step interface {
	isCleaningStep()
}

type StepMarkHostCleaning struct{}

func (StepMarkHostCleaning) isCleaningStep() {}

type StepStartOperation struct {
	Operation *infrastructurev1beta1.TartHostOperation
}

func (StepStartOperation) isCleaningStep() {}

type StepPersistPlan struct {
	Operation *infrastructurev1beta1.TartHostOperation
	Plan      agentprotocol.ValidatedPlan
	Signature agentprotocol.Signature
}

func (StepPersistPlan) isCleaningStep() {}

type Event interface {
	isCleaningEvent()
}

type EventOperationStarted struct {
	OperationID string
}

func (EventOperationStarted) isCleaningEvent() {}

type EventPlanPersisted struct {
	OperationID string
}

func (EventPlanPersisted) isCleaningEvent() {}

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
		hostPhase:  hostPhase,
		operations: operations,
		plans:      plans,
		signer:     signer,
		now:        time.Now,
	}
}

func (workflow *Workflow) StartCleaning(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
) (*infrastructurev1beta1.TartHostOperation, error) {
	if err := workflow.hostPhase.MarkHostCleaningForDeletion(ctx, host, machine.Spec.DeletionPolicy); err != nil {
		return nil, fmt.Errorf("mark TartHost cleaning: %w", err)
	}
	draft, err := BuildOperationDraft(machine, host, "", workflow.now())
	if err != nil {
		return nil, err
	}
	candidatePlan, err := workflow.buildPlan(host, machine.Spec.DeletionPolicy, draft)
	if err != nil {
		return nil, err
	}
	draft.Spec.PlanDigest = candidatePlan.Digest.String()

	started, err := workflow.operations.Start(ctx, draft)
	if err != nil {
		return nil, fmt.Errorf("start Cleaning operation: %w", err)
	}
	persistedPlan, err := workflow.buildPlan(host, machine.Spec.DeletionPolicy, started)
	if err != nil {
		return nil, err
	}
	if persistedPlan.Digest.String() != started.Spec.PlanDigest {
		return nil, fmt.Errorf("stored Cleaning Operation Plan digest does not match regenerated Plan")
	}
	if err := workflow.plans.Write(ctx, started, persistedPlan.Plan, persistedPlan.Signature); err != nil {
		return nil, fmt.Errorf("persist Cleaning Plan: %w", err)
	}
	return started, nil
}

func (workflow *Workflow) buildPlan(
	host *infrastructurev1beta1.TartHost,
	policy infrastructurev1beta1.DeletionPolicy,
	operation *infrastructurev1beta1.TartHostOperation,
) (SignedCleaningPlan, error) {
	plan, err := BuildCleaningPlan(CleaningPlanInput{
		OperationID:    operation.Spec.OperationID,
		Host:           host,
		DeletionPolicy: policy,
		Deadline:       operation.Spec.Deadline.Time,
	}, workflow.signer.KeyID, workflow.signer.PrivateKey)
	if err != nil {
		return SignedCleaningPlan{}, fmt.Errorf("build signed Cleaning Plan: %w", err)
	}
	return plan, nil
}
