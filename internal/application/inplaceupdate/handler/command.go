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

	"github.com/opencontainers/go-digest"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	inplaceupdatestep "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate/step"
	nodelifecycleapp "github.com/walnuts1018/cluster-api-provider-tart/internal/application/nodelifecycle"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

type Steps interface {
	StartOperation(
		context.Context,
		inplaceupdatestep.StartOperation,
	) (*infrastructurev1beta1.TartHostOperation, error)
	PersistAgentPlan(context.Context, inplaceupdatestep.PersistAgentPlan) error
	PersistNodeLifecyclePlan(context.Context, inplaceupdatestep.PersistNodeLifecyclePlan) error
}

type CommandHandler struct {
	steps Steps
}

type SignedAgentPlan struct {
	Plan      agentprotocol.ValidatedPlan
	Signature agentprotocol.Signature
	Digest    digest.Digest
}

type SignedNodeLifecyclePlan struct {
	Plan      nodelifecycleapp.ValidatedPlan
	Signature agentprotocol.Signature
	Digest    digest.Digest
}

type StartCommand struct {
	Operation              *infrastructurev1beta1.TartHostOperation
	BuildAgentPlan         func(*infrastructurev1beta1.TartHostOperation) (SignedAgentPlan, error)
	BuildNodeLifecyclePlan func(*infrastructurev1beta1.TartHostOperation) (SignedNodeLifecyclePlan, bool, error)
}

func NewCommandHandler(steps Steps) *CommandHandler {
	return &CommandHandler{steps: steps}
}

func (handler *CommandHandler) Start(
	ctx context.Context,
	command StartCommand,
) (*infrastructurev1beta1.TartHostOperation, bool, error) {
	if command.BuildAgentPlan == nil {
		return nil, false, fmt.Errorf("build Update Plan step is not configured")
	}
	if command.BuildNodeLifecyclePlan == nil {
		return nil, false, fmt.Errorf("build Node Lifecycle Plan step is not configured")
	}
	candidatePlan, err := command.BuildAgentPlan(command.Operation)
	if err != nil {
		return nil, false, err
	}
	command.Operation.Spec.PlanDigest = candidatePlan.Digest.String()
	candidateNodePlan, hasCandidateNodePlan, err := command.BuildNodeLifecyclePlan(command.Operation)
	if err != nil {
		return nil, false, err
	}
	if hasCandidateNodePlan {
		command.Operation.Spec.NodeLifecyclePlanDigest = candidateNodePlan.Digest.String()
	}

	started, err := handler.StartOperation(ctx, inplaceupdatestep.StartOperation{Operation: command.Operation})
	if err != nil {
		return nil, false, err
	}
	persistedPlan, err := command.BuildAgentPlan(started)
	if err != nil {
		return nil, false, err
	}
	if persistedPlan.Digest.String() != started.Spec.PlanDigest {
		return nil, false, fmt.Errorf("stored Update Operation Plan digest does not match regenerated Plan")
	}
	persistedNodePlan, hasPersistedNodePlan, err := command.BuildNodeLifecyclePlan(started)
	if err != nil {
		return nil, false, err
	}
	if hasPersistedNodePlan && persistedNodePlan.Digest.String() != started.Spec.NodeLifecyclePlanDigest {
		return nil, false, fmt.Errorf("stored Update Operation Node Lifecycle Plan digest does not match regenerated Plan")
	}
	if err := handler.PersistAgentPlan(ctx, inplaceupdatestep.PersistAgentPlan{
		Operation: started,
		Plan:      persistedPlan.Plan,
		Signature: persistedPlan.Signature,
	}); err != nil {
		return nil, false, err
	}
	if hasPersistedNodePlan {
		if err := handler.PersistNodeLifecyclePlan(ctx, inplaceupdatestep.PersistNodeLifecyclePlan{
			Operation: started,
			Plan:      persistedNodePlan.Plan,
			Signature: persistedNodePlan.Signature,
		}); err != nil {
			return nil, false, err
		}
	}
	return started, hasPersistedNodePlan, nil
}

func (handler *CommandHandler) StartOperation(
	ctx context.Context,
	command inplaceupdatestep.StartOperation,
) (*infrastructurev1beta1.TartHostOperation, error) {
	operation, err := handler.steps.StartOperation(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("start Update Operation: %w", err)
	}
	return operation, nil
}

func (handler *CommandHandler) PersistAgentPlan(
	ctx context.Context,
	command inplaceupdatestep.PersistAgentPlan,
) error {
	if err := handler.steps.PersistAgentPlan(ctx, command); err != nil {
		return fmt.Errorf("persist Update Plan: %w", err)
	}
	return nil
}

func (handler *CommandHandler) PersistNodeLifecyclePlan(
	ctx context.Context,
	command inplaceupdatestep.PersistNodeLifecyclePlan,
) error {
	if err := handler.steps.PersistNodeLifecyclePlan(ctx, command); err != nil {
		return fmt.Errorf("persist Node Lifecycle Plan: %w", err)
	}
	return nil
}
