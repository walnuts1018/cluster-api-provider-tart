/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package extension

import (
	"context"

	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

// HandleUpdateMachine handles the UpdateMachine hook.
// Since cluster-api-provider-tart only manages OS provisioning via iPXE,
// it does not perform any in-place OS updates. This hook simply acknowledges
// the update request and returns success immediately.
func HandleUpdateMachine(ctx context.Context, req *runtimehooksv1.UpdateMachineRequest, resp *runtimehooksv1.UpdateMachineResponse) {
	log := ctrllog.FromContext(ctx)

	log.Info("UpdateMachine called - no action required for Tart provider",
		"machine", req.Desired.Machine.Name,
	)

	// Tart provider does not manage OS-level changes.
	// The infrastructure machine (TartMachine) does not need any update
	// as it only tracks provisioning configuration (Image, KernelParams).
	// Any actual in-place updates are handled by the bootstrap provider (e.g., Talos).
	resp.SetStatus(runtimehooksv1.ResponseStatusSuccess)
	resp.SetMessage("no in-place update required for Tart infrastructure machine")
	resp.SetRetryAfterSeconds(0)
}
