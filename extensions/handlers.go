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

package extensions

import (
	"context"

	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
)

const notImplementedMessage = "Tart in-place update safe-diff evaluation is not implemented yet; this is a safe veto, not a signal to fall back to replacement."

// canUpdateMachine always vetoes. Until real safe-diff evaluation exists, treating any
// diff as unsafe is the only sound default: a false Success would let CAPI apply a
// partial patch and silently leave the remaining diff uncovered.
func canUpdateMachine(_ context.Context, _ *runtimehooksv1.CanUpdateMachineRequest, resp *runtimehooksv1.CanUpdateMachineResponse) {
	resp.Status = runtimehooksv1.ResponseStatusFailure
	resp.Message = notImplementedMessage
}

// canUpdateMachineSet always vetoes, for the same reason as canUpdateMachine.
func canUpdateMachineSet(_ context.Context, _ *runtimehooksv1.CanUpdateMachineSetRequest, resp *runtimehooksv1.CanUpdateMachineSetResponse) {
	resp.Status = runtimehooksv1.ResponseStatusFailure
	resp.Message = notImplementedMessage
}

// updateMachine always fails. It must never be reached in practice because
// canUpdateMachine never returns Success, but it fails closed if it is.
func updateMachine(_ context.Context, _ *runtimehooksv1.UpdateMachineRequest, resp *runtimehooksv1.UpdateMachineResponse) {
	resp.Status = runtimehooksv1.ResponseStatusFailure
	resp.Message = notImplementedMessage
}
