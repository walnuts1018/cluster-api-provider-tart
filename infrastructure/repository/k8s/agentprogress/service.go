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

package agentprogress

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	agentprogressdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/agentprogress"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/shared/option"
)

var ErrOperationNotFound = errors.New("operation or plan not found")

type Result struct {
	Decision       agentprogressdomain.Decision
	AgentSequence  int64
	Progress       *agentprogressdomain.Progress
	CompletedSteps []string
}

type Service struct {
	client client.Client
}

func NewService(k8sClient client.Client) *Service {
	return &Service{client: k8sClient}
}

func (service *Service) Report(
	ctx context.Context,
	key client.ObjectKey,
	operationUID, planDigest string,
	sequence int64,
	progress agentprogressdomain.Progress,
) (Result, error) {
	var result Result
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		operation := &infrastructurev1beta1.TartHostOperation{}
		if err := service.client.Get(ctx, key, operation); err != nil {
			if apierrors.IsNotFound(err) {
				return ErrOperationNotFound
			}
			return err
		}
		if operation.Spec.OperationID != operationUID || operation.Spec.PlanDigest != planDigest {
			return ErrOperationNotFound
		}

		evaluation := agentprogressdomain.Evaluate(statusState(operation), sequence, progress)
		switch evaluation.Decision {
		case agentprogressdomain.DecisionDuplicate, agentprogressdomain.DecisionGap, agentprogressdomain.DecisionInvalid:
			result = statusResult(operation, evaluation.Decision)
			return nil
		case agentprogressdomain.DecisionApply:
			operation.Status.AgentSequence = evaluation.State.Sequence
			operation.Status.CompletedSteps = evaluation.State.CompletedSteps
			operation.Status.AgentProgress = &infrastructurev1beta1.AgentProgressStatus{
				Step:     progress.Step,
				DiskRole: progress.DiskRole,
				Percent:  progress.Percent,
			}
			operation.Status.Phase = infrastructurev1beta1.TartHostOperationPhase(
				agentprogressdomain.NextPhase(string(operation.Status.Phase), progress),
			)
			if err := service.client.Status().Update(ctx, operation); err != nil {
				return err
			}
			result = statusResult(operation, evaluation.Decision)
			return nil
		}
		return fmt.Errorf("unsupported progress decision: %q", evaluation.Decision)
	})
	if err != nil {
		return Result{}, fmt.Errorf("record agent progress: %w", err)
	}
	return result, nil
}

func statusState(operation *infrastructurev1beta1.TartHostOperation) agentprogressdomain.State {
	state := agentprogressdomain.State{
		Sequence:       operation.Status.AgentSequence,
		CompletedSteps: operation.Status.CompletedSteps,
	}
	if operation.Status.AgentProgress != nil {
		state.Progress = option.Some(agentprogressdomain.Progress{
			Step:     operation.Status.AgentProgress.Step,
			DiskRole: operation.Status.AgentProgress.DiskRole,
			Percent:  operation.Status.AgentProgress.Percent,
		})
	}
	return state
}

func statusResult(
	operation *infrastructurev1beta1.TartHostOperation,
	decision agentprogressdomain.Decision,
) Result {
	result := Result{
		Decision:       decision,
		AgentSequence:  operation.Status.AgentSequence,
		CompletedSteps: append([]string(nil), operation.Status.CompletedSteps...),
	}
	if operation.Status.AgentProgress != nil {
		result.Progress = &agentprogressdomain.Progress{
			Step:     operation.Status.AgentProgress.Step,
			DiskRole: operation.Status.AgentProgress.DiskRole,
			Percent:  operation.Status.AgentProgress.Percent,
		}
	}
	return result
}
