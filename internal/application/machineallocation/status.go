package machineallocation

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

const (
	ReadyCondition           = "Ready"
	AllocationConflictReason = "AllocationConflict"
)

func StatusWithAllocationConflict(
	machine *infrastructurev1beta1.TartMachine,
	message string,
) infrastructurev1beta1.TartMachineStatus {
	status := *machine.Status.DeepCopy()
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               ReadyCondition,
		Status:             metav1.ConditionFalse,
		Reason:             AllocationConflictReason,
		Message:            message,
		ObservedGeneration: machine.Generation,
	})
	status.ObservedGeneration = machine.Generation
	return status
}
