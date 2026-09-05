package extension

import (
	"context"
	"fmt"

	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

const updateMachineRetryAfterSeconds int32 = 10

// UpdateStarterはUpdateMachine requestを永続Operationへ接続する境界である。
type UpdateStarter interface {
	Start(
		context.Context,
		*runtimehooksv1.UpdateMachineRequest,
	) (*infrastructurev1beta1.TartHostOperation, error)
}

// UpdateMachineHandlerはUpdate開始とRuntime Hook responseの変換を担当する。
type UpdateMachineHandler struct {
	starter UpdateStarter
	support *TargetSupportChecker
}

// NewUpdateMachineHandlerはUpdateMachine handlerを生成する。
func NewUpdateMachineHandler(starter UpdateStarter) *UpdateMachineHandler {
	return &UpdateMachineHandler{starter: starter}
}

// NewUpdateMachineHandlerWithSupportは対象gate判定付きhandlerを生成する。
func NewUpdateMachineHandlerWithSupport(
	starter UpdateStarter,
	support *TargetSupportChecker,
) *UpdateMachineHandler {
	return &UpdateMachineHandler{
		starter: starter,
		support: support,
	}
}

// HandleはOperationを開始または再取得し、永続化済みphaseをCAPIへ返す。
func (handler *UpdateMachineHandler) Handle(
	ctx context.Context,
	request *runtimehooksv1.UpdateMachineRequest,
	response *runtimehooksv1.UpdateMachineResponse,
) {
	if handler.support != nil {
		supported, reason, err := handler.support.SupportsMachine(ctx, &request.Desired.Machine)
		if err != nil {
			ctrllog.FromContext(ctx).Error(err, "Failed to evaluate in-place update target")
			response.SetStatus(runtimehooksv1.ResponseStatusFailure)
			response.SetMessage(fmt.Sprintf("failed to evaluate in-place update target: %v", err))
			response.SetRetryAfterSeconds(0)
			return
		}
		if !supported {
			response.SetStatus(runtimehooksv1.ResponseStatusFailure)
			response.SetMessage("in-place update target is disabled: " + reason)
			response.SetRetryAfterSeconds(0)
			return
		}
	}

	operation, err := handler.starter.Start(ctx, request)
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "Failed to start in-place update")
		response.SetStatus(runtimehooksv1.ResponseStatusFailure)
		response.SetMessage(fmt.Sprintf("failed to start in-place update: %v", err))
		response.SetRetryAfterSeconds(0)
		return
	}
	if operation == nil {
		response.SetStatus(runtimehooksv1.ResponseStatusFailure)
		response.SetMessage("failed to start in-place update: operation is missing")
		response.SetRetryAfterSeconds(0)
		return
	}
	mapUpdateOperationResponse(operation, response)
}

func mapUpdateOperationResponse(
	operation *infrastructurev1beta1.TartHostOperation,
	response *runtimehooksv1.UpdateMachineResponse,
) {
	switch operation.Status.Phase {
	case "",
		infrastructurev1beta1.TartHostOperationPhasePending,
		infrastructurev1beta1.TartHostOperationPhasePreparingBoot,
		infrastructurev1beta1.TartHostOperationPhaseWaitingForAgent,
		infrastructurev1beta1.TartHostOperationPhaseWriting,
		infrastructurev1beta1.TartHostOperationPhaseVerifying,
		infrastructurev1beta1.TartHostOperationPhaseBootTrial,
		infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
		infrastructurev1beta1.TartHostOperationPhaseDistributionUpdating,
		infrastructurev1beta1.TartHostOperationPhaseRollingBack:
		response.SetStatus(runtimehooksv1.ResponseStatusSuccess)
		response.SetMessage("in-place update is in progress")
		response.SetRetryAfterSeconds(updateMachineRetryAfterSeconds)
	case infrastructurev1beta1.TartHostOperationPhaseSucceeded:
		response.SetStatus(runtimehooksv1.ResponseStatusSuccess)
		response.SetMessage("in-place update completed")
		response.SetRetryAfterSeconds(0)
	case infrastructurev1beta1.TartHostOperationPhaseFailed,
		infrastructurev1beta1.TartHostOperationPhaseRecoveryRequired:
		response.SetStatus(runtimehooksv1.ResponseStatusFailure)
		response.SetMessage(fmt.Sprintf("in-place update ended in phase %s", operation.Status.Phase))
		response.SetRetryAfterSeconds(0)
	default:
		response.SetStatus(runtimehooksv1.ResponseStatusFailure)
		response.SetMessage(fmt.Sprintf("in-place update has unsupported phase %q", operation.Status.Phase))
		response.SetRetryAfterSeconds(0)
	}
}
