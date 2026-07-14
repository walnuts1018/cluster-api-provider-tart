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
	machineexecutionmodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution/model"
)

type ProvisionProgressSteps interface {
	ResolveProvisionProgressReference(
		context.Context,
		*infrastructurev1beta1.TartMachine,
	) (machineexecutionmodel.ProvisionProgressReferenceResult, error)
	ClearStaleProvisionOperationReference(
		context.Context,
		*infrastructurev1beta1.TartMachine,
		*infrastructurev1beta1.ResourceReference,
	) error
	DecideProvisionProgress(
		*infrastructurev1beta1.TartHostOperation,
	) (machineexecutionmodel.ProvisionProgressDecisionResult, error)
	PatchProvisionFailureStatus(
		context.Context,
		*infrastructurev1beta1.TartMachine,
		machineexecutionmodel.ProvisionProgressFailed,
	) error
}

type ProvisionProgressHandler struct {
	steps ProvisionProgressSteps
}

func NewProvisionProgressHandler(steps ProvisionProgressSteps) *ProvisionProgressHandler {
	return &ProvisionProgressHandler{steps: steps}
}

func (handler *ProvisionProgressHandler) Handle(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
) error {
	referenceResult, err := handler.steps.ResolveProvisionProgressReference(ctx, machine)
	if err != nil {
		return err
	}
	switch reference := referenceResult.(type) {
	case machineexecutionmodel.ProvisionProgressReferenceStale:
		return handler.steps.ClearStaleProvisionOperationReference(ctx, machine, reference.Reference)
	case machineexecutionmodel.ProvisionProgressReferenceAbsent:
		return nil
	case machineexecutionmodel.ProvisionProgressReferenceResolved:
		return handler.handleResolved(ctx, machine, reference.Operation)
	default:
		return fmt.Errorf("unknown Operation reference result for provision progress: %T", referenceResult)
	}
}

func (handler *ProvisionProgressHandler) handleResolved(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	decision, err := handler.steps.DecideProvisionProgress(operation)
	if err != nil {
		return fmt.Errorf("decide TartHostOperation progress: %w", err)
	}
	switch decision := decision.(type) {
	case machineexecutionmodel.ProvisionProgressFailed:
		return handler.steps.PatchProvisionFailureStatus(ctx, machine, decision)
	case machineexecutionmodel.ProvisionProgressAwaitingHealth:
		return nil
	default:
		return fmt.Errorf("unexpected TartMachine provisioning progress decision: %T", decision)
	}
}
