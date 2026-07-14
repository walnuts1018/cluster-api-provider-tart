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
	machinedeletionmodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinedeletion/model"
	machinedeletionstep "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinedeletion/step"
	machinedeletiondomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinedeletion"
)

type CommandHandler struct {
	steps *machinedeletionstep.Executor
}

func NewCommandHandler(steps *machinedeletionstep.Executor) *CommandHandler {
	return &CommandHandler{steps: steps}
}

func (handler *CommandHandler) Handle(
	ctx context.Context,
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
	command machinedeletiondomain.Command,
) (machinedeletionmodel.Result, error) {
	switch command := command.(type) {
	case machinedeletiondomain.CommandReleaseFinalizer:
		return machinedeletionmodel.ResultFinalizerReady{}, nil
	case machinedeletiondomain.CommandStartCleaning:
		if host == nil {
			return nil, fmt.Errorf("TartHost is required to start Cleaning operation")
		}
		return machinedeletionmodel.ResultWaiting{}, handler.steps.StartCleaning(ctx, machine, host)
	case machinedeletiondomain.CommandClearOperationReference:
		return machinedeletionmodel.ResultWaiting{}, handler.steps.ClearOperationReference(ctx, machine)
	case machinedeletiondomain.CommandWaitCleaning:
		return machinedeletionmodel.ResultWaiting{}, nil
	case machinedeletiondomain.CommandFailCleaning:
		return nil, fmt.Errorf("Cleaning operation finished in %s", command.Phase)
	default:
		return nil, fmt.Errorf("unknown TartMachine deletion command: %T", command)
	}
}
