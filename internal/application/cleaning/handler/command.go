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
	"time"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	cleaningevent "github.com/walnuts1018/cluster-api-provider-tart/internal/application/cleaning/event"
	cleaningmodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/cleaning/model"
	cleaningstep "github.com/walnuts1018/cluster-api-provider-tart/internal/application/cleaning/step"
)

type Steps interface {
	MarkHostCleaning(
		context.Context,
		*infrastructurev1beta1.TartHost,
		infrastructurev1beta1.DeletionPolicy,
	) error
	StartOperation(
		context.Context,
		cleaningstep.StartOperation,
	) (*infrastructurev1beta1.TartHostOperation, error)
	PersistPlan(context.Context, cleaningstep.PersistPlan) error
	BuildSignedCleaningPlan(
		*infrastructurev1beta1.TartHost,
		infrastructurev1beta1.DeletionPolicy,
		*infrastructurev1beta1.TartHostOperation,
	) (cleaningstep.SignedCleaningPlan, error)
}

type CommandHandler struct {
	steps Steps
}

func NewCommandHandler(steps Steps) *CommandHandler {
	return &CommandHandler{steps: steps}
}

func (handler *CommandHandler) StartCleaning(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
	now time.Time,
) (cleaningmodel.StartResult, error) {
	if err := handler.MarkHostCleaning(ctx, host, machine.Spec.DeletionPolicy); err != nil {
		return cleaningmodel.StartResult{}, err
	}
	draft, err := cleaningstep.BuildOperationDraft(machine, host, "", now)
	if err != nil {
		return cleaningmodel.StartResult{}, err
	}
	candidatePlan, err := handler.steps.BuildSignedCleaningPlan(host, machine.Spec.DeletionPolicy, draft)
	if err != nil {
		return cleaningmodel.StartResult{}, err
	}
	draft.Spec.PlanDigest = candidatePlan.Digest.String()

	started, err := handler.StartOperation(ctx, cleaningstep.StartOperation{Operation: draft})
	if err != nil {
		return cleaningmodel.StartResult{}, err
	}
	persistedPlan, err := handler.steps.BuildSignedCleaningPlan(host, machine.Spec.DeletionPolicy, started)
	if err != nil {
		return cleaningmodel.StartResult{}, err
	}
	if persistedPlan.Digest.String() != started.Spec.PlanDigest {
		return cleaningmodel.StartResult{}, fmt.Errorf("stored Cleaning Operation Plan digest does not match regenerated Plan")
	}
	if err := handler.PersistPlan(ctx, cleaningstep.PersistPlan{
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

func (handler *CommandHandler) MarkHostCleaning(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	deletionPolicy infrastructurev1beta1.DeletionPolicy,
) error {
	if err := handler.steps.MarkHostCleaning(ctx, host, deletionPolicy); err != nil {
		return fmt.Errorf("mark TartHost cleaning: %w", err)
	}
	return nil
}

func (handler *CommandHandler) StartOperation(
	ctx context.Context,
	command cleaningstep.StartOperation,
) (*infrastructurev1beta1.TartHostOperation, error) {
	operation, err := handler.steps.StartOperation(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("start Cleaning operation: %w", err)
	}
	return operation, nil
}

func (handler *CommandHandler) PersistPlan(ctx context.Context, command cleaningstep.PersistPlan) error {
	if err := handler.steps.PersistPlan(ctx, command); err != nil {
		return fmt.Errorf("persist Cleaning Plan: %w", err)
	}
	return nil
}
