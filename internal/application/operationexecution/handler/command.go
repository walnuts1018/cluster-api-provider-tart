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

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	operationexecutionmodel "github.com/walnuts1018/cluster-api-provider-tart/internal/application/operationexecution/model"
	operationexecutionstep "github.com/walnuts1018/cluster-api-provider-tart/internal/application/operationexecution/step"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

type CommandHandler struct {
	steps *operationexecutionstep.Executor
}

func NewCommandHandler(steps *operationexecutionstep.Executor) *CommandHandler {
	return &CommandHandler{steps: steps}
}

func (handler *CommandHandler) Handle(
	ctx context.Context,
	operation *infrastructurev1beta1.TartHostOperation,
	command operationdomain.Command,
) (operationexecutionmodel.Result, error) {
	log := logf.FromContext(ctx)

	switch selected := command.(type) {
	case operationdomain.CommandInitializePending:
		return operationexecutionmodel.Result{}, handler.steps.TransitionPhase(ctx, operation, apiOperationPhase(selected.Target))
	case operationdomain.CommandPrepareBoot:
		return operationexecutionmodel.Result{}, handler.steps.PrepareOperationBoot(ctx, operation, selected.Host)
	case operationdomain.CommandObserveActive:
		if err := handler.steps.ObserveActiveOperationDriverState(ctx, operation); err != nil {
			return operationexecutionmodel.Result{}, err
		}
		return operationexecutionmodel.Result{RequeueAfter: operationexecutionmodel.DeadlineRequeueInterval}, nil
	case operationdomain.CommandAwaitMachineHealth:
		return operationexecutionmodel.Result{RequeueAfter: operationexecutionmodel.DeadlineRequeueInterval}, nil
	case operationdomain.CommandCompleteWipeAll:
		return operationexecutionmodel.Result{}, handler.steps.CompleteOperationWithHostCommand(ctx, operation, selected.Host, selected.Target)
	case operationdomain.CommandCompleteCleaning:
		return operationexecutionmodel.Result{}, handler.steps.CompleteOperationWithHostCommand(ctx, operation, selected.Host, selected.Target)
	case operationdomain.CommandHandleTerminal:
		return operationexecutionmodel.Result{}, handler.steps.ApplyHostCommand(ctx, operation, selected.Host)
	case operationdomain.CommandFailDeadlineExceeded:
		log.Info("TartHostOperation deadline exceeded",
			"operation", client.ObjectKeyFromObject(operation).String(),
			"deadline", operation.Spec.Deadline.Time,
			"phase", operation.Status.Phase,
		)
		return operationexecutionmodel.Result{}, handler.steps.ApplyDeadlineOutcome(ctx, operation, selected.Outcome)
	case operationdomain.CommandIgnore:
		log.V(4).Info("TartHostOperation in unhandled phase, skipping",
			"operation", client.ObjectKeyFromObject(operation).String(),
			"phase", operation.Status.Phase,
		)
		return operationexecutionmodel.Result{}, nil
	default:
		return operationexecutionmodel.Result{}, fmt.Errorf("unknown TartHostOperation workflow command %T", selected)
	}
}

func apiOperationPhase(phase operationdomain.Phase) infrastructurev1beta1.TartHostOperationPhase {
	return infrastructurev1beta1.TartHostOperationPhase(phase)
}
