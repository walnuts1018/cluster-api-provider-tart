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

package extension

import (
	"context"
	"fmt"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	domain "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/entity/inplaceupdate"
)

// CanUpdateMachineSetHandlerはOSOnly allowlistと対象gateを束ねる。
type CanUpdateMachineSetHandler struct {
	support *TargetSupportChecker
}

// NewCanUpdateMachineSetHandlerはMachineSet hook handlerを生成する。
func NewCanUpdateMachineSetHandler(support *TargetSupportChecker) *CanUpdateMachineSetHandler {
	return &CanUpdateMachineSetHandler{support: support}
}

// HandleはOSOnly allowlistでtemplate差分を分類する。
func (handler *CanUpdateMachineSetHandler) Handle(
	ctx context.Context,
	request *runtimehooksv1.CanUpdateMachineSetRequest,
	response *runtimehooksv1.CanUpdateMachineSetResponse,
) {
	supported, reason, err := handler.support.SupportsMachineSet(ctx, &request.Desired.MachineSet)
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "Failed to evaluate in-place MachineSet target")
		response.SetStatus(runtimehooksv1.ResponseStatusFailure)
		response.SetMessage("failed to evaluate in-place update target: " + err.Error())
		return
	}
	if !supported {
		response.SetStatus(runtimehooksv1.ResponseStatusSuccess)
		response.SetMessage("in-place update not selected; " + reason)
		return
	}

	classification, desired, err := classifyMachineSetRequest(request)
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "Failed to classify in-place MachineSet update")
		response.SetStatus(runtimehooksv1.ResponseStatusFailure)
		response.SetMessage("failed to classify in-place update: " + err.Error())
		return
	}
	classification, lifecycleSupported, err := allowMachineSetLifecycleFieldsIfEligible(ctx, handler.support, request.Desired.MachineSet, classification)
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "Failed to evaluate node lifecycle target")
		response.SetStatus(runtimehooksv1.ResponseStatusFailure)
		response.SetMessage("failed to evaluate node lifecycle target: " + err.Error())
		return
	}
	if lifecycleSupported {
		classification = classificationWithLifecycleFieldsAllowed(classification)
	}
	if !classification.CanUpdateInPlace() {
		response.SetStatus(runtimehooksv1.ResponseStatusSuccess)
		response.SetMessage(fmt.Sprintf(
			"in-place update not selected; rejected fields: %v",
			classification.Rejected,
		))
		return
	}
	patch, err := machineTemplatePatch(classification, desired)
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "Failed to build infrastructure machine template patch")
		response.SetStatus(runtimehooksv1.ResponseStatusFailure)
		response.SetMessage("failed to build infrastructure machine template patch: " + err.Error())
		return
	}
	response.InfrastructureMachineTemplatePatch = patch
	bootstrapPatch, err := machineSetBootstrapTemplatePatch(classification, request.Desired.BootstrapConfigTemplate)
	if err != nil {
		ctrllog.FromContext(ctx).Error(err, "Failed to build bootstrap config template patch")
		response.SetStatus(runtimehooksv1.ResponseStatusFailure)
		response.SetMessage("failed to build bootstrap config template patch: " + err.Error())
		return
	}
	response.BootstrapConfigTemplatePatch = bootstrapPatch
	response.SetStatus(runtimehooksv1.ResponseStatusSuccess)
	response.SetMessage("in-place update is supported")
}

func allowMachineSetLifecycleFieldsIfEligible(
	ctx context.Context,
	support *TargetSupportChecker,
	machineSet clusterv1.MachineSet,
	classification domain.Classification,
) (domain.Classification, bool, error) {
	if !classificationContainsOnlyLifecycleRejects(classification) {
		return classification, false, nil
	}
	supported, _, err := support.SupportsNodeLifecycleMachineSet(ctx, &machineSet)
	if err != nil {
		return domain.Classification{}, false, err
	}
	if !supported {
		return classification, false, nil
	}
	return classification, true, nil
}
