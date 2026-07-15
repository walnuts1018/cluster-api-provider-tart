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

package operationexecution

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appupdate "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

const cleaningPolicyRequiredReason = "CleaningPolicyRequired"

func TestPresentは再実行待ちとFailureを写像する(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  operationdomain.Result
		assert func(*testing.T, Result)
	}{
		{
			name:  "ObserveActiveはdeadline間隔でrequeue",
			input: operationdomain.ObserveActive{},
			assert: func(t *testing.T, result Result) {
				t.Helper()
				if result.RequeueAfter != DeadlineRequeueInterval {
					t.Fatalf("RequeueAfter = %s, want %s", result.RequeueAfter, DeadlineRequeueInterval)
				}
			},
		},
		{
			name: "CleaningPolicyRequiredはpresentationへ変換",
			input: operationdomain.Rejected{
				Failure: operationdomain.CleaningPolicyRequired{
					Kind:  operationdomain.KindClean,
					Phase: operationdomain.PhaseAwaitingHealth,
				},
			},
			assert: func(t *testing.T, result Result) {
				t.Helper()
				if result.StatusCondition == nil {
					t.Fatal("StatusCondition = nil, want Degraded condition")
				}
				if result.StatusCondition.Type != appupdate.ConditionDegraded {
					t.Fatalf("condition type = %q, want %q", result.StatusCondition.Type, appupdate.ConditionDegraded)
				}
				if result.StatusCondition.Status != metav1.ConditionTrue {
					t.Fatalf("condition status = %q, want True", result.StatusCondition.Status)
				}
				if result.StatusCondition.Reason != cleaningPolicyRequiredReason {
					t.Fatalf("condition reason = %q, want %s", result.StatusCondition.Reason, cleaningPolicyRequiredReason)
				}
				if result.Event == nil || result.Event.Type != "Warning" || result.Event.Reason != cleaningPolicyRequiredReason {
					t.Fatalf("Event = %#v, want Warning %s", result.Event, cleaningPolicyRequiredReason)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.assert(t, present(tt.input))
		})
	}
}
