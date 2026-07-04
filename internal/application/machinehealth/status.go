package machinehealth

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
)

const ReadyCondition = "Ready"

func StatusWithNodeHealth(
	machine *infrastructurev1beta1.TartMachine,
	observation machinehealthdomain.NodeObservation,
) infrastructurev1beta1.TartMachineStatus {
	status := *machine.Status.DeepCopy()
	result := machinehealthdomain.EvaluateNode(observation)
	conditionStatus := metav1.ConditionFalse
	if result.Ready {
		conditionStatus = metav1.ConditionTrue
	}
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               ReadyCondition,
		Status:             conditionStatus,
		Reason:             string(result.Reason),
		Message:            result.Message,
		ObservedGeneration: machine.Generation,
	})
	status.ObservedGeneration = machine.Generation
	return status
}
