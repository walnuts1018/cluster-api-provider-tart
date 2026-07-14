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

package step

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution/model"
)

func TestPlanOperationPhaseTransition(t *testing.T) {
	tests := []struct {
		name       string
		operation  infrastructurev1beta1.TartHostOperation
		target     infrastructurev1beta1.TartHostOperationPhase
		wantResult any
	}{
		{
			name: "phase and observed generation already match",
			operation: infrastructurev1beta1.TartHostOperation{
				ObjectMeta: metav1.ObjectMeta{Generation: 3},
				Status: infrastructurev1beta1.TartHostOperationStatus{
					Phase:              infrastructurev1beta1.TartHostOperationPhaseSucceeded,
					ObservedGeneration: 3,
				},
			},
			target:     infrastructurev1beta1.TartHostOperationPhaseSucceeded,
			wantResult: model.OperationStatusPatchAlreadyApplied{},
		},
		{
			name: "phase is changed and observed generation catches up",
			operation: infrastructurev1beta1.TartHostOperation{
				ObjectMeta: metav1.ObjectMeta{Generation: 3},
				Status: infrastructurev1beta1.TartHostOperationStatus{
					Phase:              infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
					ObservedGeneration: 2,
				},
			},
			target:     infrastructurev1beta1.TartHostOperationPhaseSucceeded,
			wantResult: model.OperationStatusPatchRequired{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operation := tt.operation.DeepCopy()

			result := PlanOperationPhaseTransition(operation, tt.target)

			if operation.Status.Phase != tt.target {
				t.Fatalf("Phase = %q, want %q", operation.Status.Phase, tt.target)
			}
			if operation.Status.ObservedGeneration != operation.Generation {
				t.Fatalf("ObservedGeneration = %d, want %d", operation.Status.ObservedGeneration, operation.Generation)
			}
			switch tt.wantResult.(type) {
			case model.OperationStatusPatchAlreadyApplied:
				if _, ok := result.(model.OperationStatusPatchAlreadyApplied); !ok {
					t.Fatalf("result = %T, want model.OperationStatusPatchAlreadyApplied", result)
				}
			case model.OperationStatusPatchRequired:
				required, ok := result.(model.OperationStatusPatchRequired)
				if !ok {
					t.Fatalf("result = %T, want model.OperationStatusPatchRequired", result)
				}
				if required.Original == nil {
					t.Fatalf("Original is nil")
				}
			default:
				t.Fatalf("unexpected wantResult = %T", tt.wantResult)
			}
		})
	}
}
