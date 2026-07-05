package agentprogress

import (
	"context"
	"errors"
	"fmt"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	agentprogressdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/agentprogress"
)

var ErrOperationNotFound = errors.New("operation or plan not found")

type Result struct {
	Decision       agentprogressdomain.Decision
	AgentSequence  int64
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
	completedStep string,
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

		decision := agentprogressdomain.EvaluateSequence(operation.Status.AgentSequence, sequence)
		switch decision {
		case agentprogressdomain.DecisionDuplicate, agentprogressdomain.DecisionGap, agentprogressdomain.DecisionInvalid:
			result = statusResult(operation, decision)
			return nil
		case agentprogressdomain.DecisionApply:
			operation.Status.AgentSequence = sequence
			operation.Status.CompletedSteps = appendStep(operation.Status.CompletedSteps, completedStep)
			if err := service.client.Status().Update(ctx, operation); err != nil {
				return err
			}
			result = statusResult(operation, decision)
			return nil
		}
		return fmt.Errorf("unsupported progress decision: %q", decision)
	})
	if err != nil {
		return Result{}, fmt.Errorf("record agent progress: %w", err)
	}
	return result, nil
}

func appendStep(steps []string, step string) []string {
	if slices.Contains(steps, step) {
		return steps
	}
	return append(steps, step)
}

func statusResult(
	operation *infrastructurev1beta1.TartHostOperation,
	decision agentprogressdomain.Decision,
) Result {
	return Result{
		Decision:       decision,
		AgentSequence:  operation.Status.AgentSequence,
		CompletedSteps: append([]string(nil), operation.Status.CompletedSteps...),
	}
}
