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

package machineallocation

import (
	"testing"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

func TestStatusWithAllocationConflict(t *testing.T) {
	t.Parallel()

	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{Generation: 4},
		Status: infrastructurev1beta1.TartMachineStatus{
			Conditions: []metav1.Condition{{
				Type:   "Provisioning",
				Status: metav1.ConditionTrue,
				Reason: "Running",
			}},
		},
	}

	status := StatusWithAllocationConflict(machine, "TartHost reference does not match")

	condition := apimeta.FindStatusCondition(status.Conditions, ReadyCondition)
	if condition == nil {
		t.Fatal("Ready condition was not set")
	}
	if condition.Status != metav1.ConditionFalse ||
		condition.Reason != AllocationConflictReason ||
		condition.Message != "TartHost reference does not match" ||
		condition.ObservedGeneration != machine.Generation {
		t.Fatalf("Ready condition = %#v", condition)
	}
	if apimeta.FindStatusCondition(status.Conditions, "Provisioning") == nil {
		t.Fatal("existing conditions were not preserved")
	}
	if status.ObservedGeneration != machine.Generation {
		t.Fatalf("observedGeneration = %d, want %d", status.ObservedGeneration, machine.Generation)
	}
}
