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
