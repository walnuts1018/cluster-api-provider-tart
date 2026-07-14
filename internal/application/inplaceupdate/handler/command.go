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

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	inplaceupdatestep "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate/step"
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

func NewCommandHandler(steps Steps) *CommandHandler {
	return &CommandHandler{steps: steps}
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
