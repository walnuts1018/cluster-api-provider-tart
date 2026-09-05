package extension

import (
	"context"
	"fmt"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	domain "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/entity/inplaceupdate"
)

// CanUpdateMachineHandlerはOSOnly allowlistと対象gateを束ねる。
type CanUpdateMachineHandler struct {
	support *TargetSupportChecker
}

// NewCanUpdateMachineHandlerはMachine hook handlerを生成する。
func NewCanUpdateMachineHandler(support *TargetSupportChecker) *CanUpdateMachineHandler {
	return &CanUpdateMachineHandler{support: support}
}

// HandleはOSOnly allowlistでcurrentとdesiredを分類する。
func (handler *CanUpdateMachineHandler) Handle(
	ctx context.Context,
	request *runtimehooksv1.CanUpdateMachineRequest,
	response *runtimehooksv1.CanUpdateMachineResponse,
) {
	supported, reason, err := handler.support.SupportsMachine(ctx, &request.Desired.Machine)
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "Failed to evaluate in-place update target")
		response.SetStatus(runtimehooksv1.ResponseStatusFailure)
		response.SetMessage("failed to evaluate in-place update target: " + err.Error())
		return
	}
	if !supported {
		response.SetStatus(runtimehooksv1.ResponseStatusSuccess)
		response.SetMessage("in-place update not selected; " + reason)
		return
	}

	classification, desired, err := classifyMachineRequest(request)
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "Failed to classify in-place Machine update")
		response.SetStatus(runtimehooksv1.ResponseStatusFailure)
		response.SetMessage("failed to classify in-place update: " + err.Error())
		return
	}
	classification, lifecycleSupported, lifecycleReason, err := allowLifecycleFieldsIfEligible(
		ctx,
		handler.support,
		request.Desired.Machine,
		classification,
	)
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "Failed to evaluate node lifecycle target")
		response.SetStatus(runtimehooksv1.ResponseStatusFailure)
		response.SetMessage("failed to evaluate node lifecycle target: " + err.Error())
		return
	}
	if lifecycleSupported {
		classification = classificationWithLifecycleFieldsAllowed(classification)
	} else if lifecycleReason != "" {
		response.SetStatus(runtimehooksv1.ResponseStatusSuccess)
		response.SetMessage("in-place update not selected; " + lifecycleReason)
		return
	}
	if !classification.CanUpdateInPlace() {
		response.SetStatus(runtimehooksv1.ResponseStatusSuccess)
		response.SetMessage(fmt.Sprintf(
			"in-place update not selected; rejected fields: %v",
			classification.Rejected,
		))
		return
	}
	patch, err := machinePatch(classification, desired.Spec)
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "Failed to build infrastructure machine patch")
		response.SetStatus(runtimehooksv1.ResponseStatusFailure)
		response.SetMessage("failed to build infrastructure machine patch: " + err.Error())
		return
	}
	response.InfrastructureMachinePatch = patch
	bootstrapPatch, err := machineBootstrapPatch(classification, request.Desired.BootstrapConfig)
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "Failed to build bootstrap config patch")
		response.SetStatus(runtimehooksv1.ResponseStatusFailure)
		response.SetMessage("failed to build bootstrap config patch: " + err.Error())
		return
	}
	response.BootstrapConfigPatch = bootstrapPatch
	response.SetStatus(runtimehooksv1.ResponseStatusSuccess)
	response.SetMessage("in-place update is supported")
}

func allowLifecycleFieldsIfEligible(
	ctx context.Context,
	support *TargetSupportChecker,
	machine clusterv1.Machine,
	classification domain.Classification,
) (domain.Classification, bool, string, error) {
	if !classificationContainsOnlyLifecycleRejects(classification) {
		return classification, false, "", nil
	}
	supported, reason, err := support.SupportsNodeLifecycleMachine(ctx, &machine)
	if err != nil {
		return domain.Classification{}, false, "", err
	}
	if !supported {
		return classification, false, reason, nil
	}
	return classification, true, "", nil
}

func classificationContainsOnlyLifecycleRejects(classification domain.Classification) bool {
	if len(classification.Rejected) == 0 {
		return false
	}
	for _, rejected := range classification.Rejected {
		if rejected != domain.FieldMachineVersion && rejected != domain.FieldBootstrapConfig {
			return false
		}
	}
	return true
}

func classificationWithLifecycleFieldsAllowed(classification domain.Classification) domain.Classification {
	if !classificationContainsOnlyLifecycleRejects(classification) {
		return classification
	}
	classification.Allowed = append(classification.Allowed, classification.Rejected...)
	classification.Rejected = classification.Rejected[:0]
	return classification
}
