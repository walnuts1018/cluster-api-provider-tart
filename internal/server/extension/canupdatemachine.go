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

	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
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
	response.SetStatus(runtimehooksv1.ResponseStatusSuccess)
	response.SetMessage("OS-only in-place update is supported")
}
