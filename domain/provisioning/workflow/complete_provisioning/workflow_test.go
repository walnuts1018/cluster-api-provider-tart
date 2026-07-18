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

package completeprovisioning

import (
	"context"
	"errors"
	"testing"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

func TestWorkflowはOperation完了後にHostをProvisionedへ遷移する(t *testing.T) {
	t.Parallel()

	var calls []string
	workflow := NewWorkflow(
		operationServiceStub{complete: func() { calls = append(calls, "operation") }},
		hostPhaseServiceStub{mark: func() { calls = append(calls, "host") }},
	)
	if outcome := workflow.Do(t.Context(), Command{
		Host:      &infrastructurev1beta1.TartHost{},
		Operation: &infrastructurev1beta1.TartHostOperation{},
	}); outcome.IsFailure() {
		t.Fatalf("Do() = %#v, want success", outcome)
	}
	if len(calls) != 2 || calls[0] != "operation" || calls[1] != "host" {
		t.Fatalf("call order = %v, want [operation host]", calls)
	}
}

func TestWorkflowはOperation完了失敗後にHostを変更しない(t *testing.T) {
	t.Parallel()

	hostMarked := false
	workflow := NewWorkflow(
		operationServiceStub{err: errors.New("operation failed")},
		hostPhaseServiceStub{mark: func() { hostMarked = true }},
	)
	if outcome := workflow.Do(t.Context(), Command{
		Host:      &infrastructurev1beta1.TartHost{},
		Operation: &infrastructurev1beta1.TartHostOperation{},
	}); !outcome.IsFailure() {
		t.Fatal("Do() succeeded unexpectedly")
	}
	if hostMarked {
		t.Fatal("host was marked after operation completion failed")
	}
}

type operationServiceStub struct {
	err      error
	complete func()
}

func (stub operationServiceStub) CompleteProvision(context.Context, *infrastructurev1beta1.TartHostOperation) error {
	if stub.complete != nil {
		stub.complete()
	}
	return stub.err
}

type hostPhaseServiceStub struct {
	mark func()
}

func (stub hostPhaseServiceStub) MarkHostProvisioned(context.Context, *infrastructurev1beta1.TartHost) error {
	if stub.mark != nil {
		stub.mark()
	}
	return nil
}
