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

package machinehealth

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
)

func TestStatusWithNodeHealthSetsReadyFalseForProviderIDMismatch(t *testing.T) {
	t.Parallel()

	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{Generation: 7},
		Spec: infrastructurev1beta1.TartMachineSpec{
			ProviderID: "tart://host-a",
		},
	}
	status := StatusWithNodeHealth(machine, machinehealthdomain.NodeObservation{
		MachineProviderID: machine.Spec.ProviderID,
		NodeProviderID:    "tart://host-b",
		NodeReady:         true,
		ObservedMachineID: "machine-id",
	})

	condition := findCondition(status.Conditions, ReadyCondition)
	if condition == nil {
		t.Fatal("Ready Conditionがありません")
	}
	if condition.Status != metav1.ConditionFalse {
		t.Fatalf("Ready status = %q, want %q", condition.Status, metav1.ConditionFalse)
	}
	if condition.Reason != string(machinehealthdomain.ReasonProviderIDMismatch) {
		t.Fatalf("Ready reason = %q, want %q", condition.Reason, machinehealthdomain.ReasonProviderIDMismatch)
	}
	if condition.ObservedGeneration != machine.Generation {
		t.Fatalf("observedGeneration = %d, want %d", condition.ObservedGeneration, machine.Generation)
	}
	if status.InstalledMachineID != "" {
		t.Fatalf("InstalledMachineID = %q, want empty", status.InstalledMachineID)
	}
}

func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}
