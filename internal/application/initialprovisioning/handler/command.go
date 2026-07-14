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
	initialprovisioningstep "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning/step"
)

type Steps interface {
	ReserveHost(
		context.Context,
		*infrastructurev1beta1.TartMachine,
		initialprovisioningstep.ReserveHost,
	) (*infrastructurev1beta1.TartHost, error)
	MarkHostReserved(
		context.Context,
		*infrastructurev1beta1.TartMachine,
		*infrastructurev1beta1.TartHost,
	) error
	StartOperation(
		context.Context,
		initialprovisioningstep.StartOperation,
	) (*infrastructurev1beta1.TartHostOperation, error)
	CompleteProvisioning(
		context.Context,
		*infrastructurev1beta1.TartHost,
		*infrastructurev1beta1.TartHostOperation,
	) error
}

type CommandHandler struct {
	steps Steps
}

func NewCommandHandler(steps Steps) *CommandHandler {
	return &CommandHandler{steps: steps}
}

func (handler *CommandHandler) ReserveHost(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	command initialprovisioningstep.ReserveHost,
) (*infrastructurev1beta1.TartHost, error) {
	host, err := handler.steps.ReserveHost(ctx, machine, command)
	if err != nil {
		return nil, fmt.Errorf("reserve TartHost: %w", err)
	}
	return host, nil
}

func (handler *CommandHandler) MarkHostReserved(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
) error {
	if err := handler.steps.MarkHostReserved(ctx, machine, host); err != nil {
		return fmt.Errorf("mark TartHost reserved: %w", err)
	}
	return nil
}

func (handler *CommandHandler) StartOperation(
	ctx context.Context,
	command initialprovisioningstep.StartOperation,
) (*infrastructurev1beta1.TartHostOperation, error) {
	operation, err := handler.steps.StartOperation(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("start TartHostOperation: %w", err)
	}
	return operation, nil
}

func (handler *CommandHandler) CompleteProvisioning(
	ctx context.Context,
	host *infrastructurev1beta1.TartHost,
	operation *infrastructurev1beta1.TartHostOperation,
) error {
	return handler.steps.CompleteProvisioning(ctx, host, operation)
}
