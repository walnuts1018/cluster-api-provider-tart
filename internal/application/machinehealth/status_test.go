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
}

func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}
