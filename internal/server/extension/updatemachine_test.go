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
	"errors"
	"testing"

	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

func TestUpdateMachineHandlerはOperationPhaseをRetryResponseへ写像する(t *testing.T) {
	tests := []struct {
		name       string
		phase      infrastructurev1beta1.TartHostOperationPhase
		status     runtimehooksv1.ResponseStatus
		retryAfter int32
	}{
		{name: "not reconciled", phase: "", status: runtimehooksv1.ResponseStatusSuccess, retryAfter: 10},
		{name: "pending", phase: infrastructurev1beta1.TartHostOperationPhasePending, status: runtimehooksv1.ResponseStatusSuccess, retryAfter: 10},
		{name: "boot trial", phase: infrastructurev1beta1.TartHostOperationPhaseBootTrial, status: runtimehooksv1.ResponseStatusSuccess, retryAfter: 10},
		{name: "rolling back", phase: infrastructurev1beta1.TartHostOperationPhaseRollingBack, status: runtimehooksv1.ResponseStatusSuccess, retryAfter: 10},
		{name: "succeeded", phase: infrastructurev1beta1.TartHostOperationPhaseSucceeded, status: runtimehooksv1.ResponseStatusSuccess},
		{name: "failed", phase: infrastructurev1beta1.TartHostOperationPhaseFailed, status: runtimehooksv1.ResponseStatusFailure},
		{name: "recovery required", phase: infrastructurev1beta1.TartHostOperationPhaseRecoveryRequired, status: runtimehooksv1.ResponseStatusFailure},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := NewUpdateMachineHandler(staticUpdateStarter{
				operation: &infrastructurev1beta1.TartHostOperation{
					Status: infrastructurev1beta1.TartHostOperationStatus{Phase: test.phase},
				},
			})
			response := &runtimehooksv1.UpdateMachineResponse{}

			handler.Handle(t.Context(), &runtimehooksv1.UpdateMachineRequest{}, response)

			if response.Status != test.status {
				t.Fatalf("Status = %q, want %q", response.Status, test.status)
			}
			if response.RetryAfterSeconds != test.retryAfter {
				t.Fatalf("RetryAfterSeconds = %d, want %d", response.RetryAfterSeconds, test.retryAfter)
			}
		})
	}
}

func TestUpdateMachineHandlerは開始失敗をFailureにする(t *testing.T) {
	handler := NewUpdateMachineHandler(staticUpdateStarter{err: errors.New("registry unavailable")})
	response := &runtimehooksv1.UpdateMachineResponse{}

	handler.Handle(t.Context(), &runtimehooksv1.UpdateMachineRequest{}, response)

	if response.Status != runtimehooksv1.ResponseStatusFailure {
		t.Fatalf("Status = %q, want Failure", response.Status)
	}
	if response.Message == "" {
		t.Fatal("Message is empty")
	}
}

type staticUpdateStarter struct {
	operation *infrastructurev1beta1.TartHostOperation
	err       error
}

func (starter staticUpdateStarter) Start(
	context.Context,
	*runtimehooksv1.UpdateMachineRequest,
) (*infrastructurev1beta1.TartHostOperation, error) {
	return starter.operation, starter.err
}
