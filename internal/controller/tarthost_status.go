package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
)

const tartHostMachineRefField = "status.machineRef"

func markHostAvailable(ctx context.Context, statusWriter client.SubResourceWriter, host *infrastructurev1alpha1.TartHost, reason, message string) error {
	original := host.DeepCopy()
	host.Status.State = infrastructurev1alpha1.TartHostStateAvailable
	host.Status.MachineRef = nil
	host.Status.ObservedGeneration = host.Generation
	apimeta.SetStatusCondition(&host.Status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: host.Generation,
	})
	return statusWriter.Patch(ctx, host, client.MergeFrom(original))
}

func tartHostMachineRefIndexValue(ref *corev1.ObjectReference) string {
	if ref == nil {
		return ""
	}
	return ref.Namespace + "/" + ref.Name + "/" + string(ref.UID)
}

func tartHostMachineRefIndexValueForMachine(machine *infrastructurev1alpha1.TartMachine) string {
	return machine.Namespace + "/" + machine.Name + "/" + string(machine.UID)
}

func IndexTartHostByMachineRef(rawObj client.Object) []string {
	host, ok := rawObj.(*infrastructurev1alpha1.TartHost)
	if !ok {
		return nil
	}
	if value := tartHostMachineRefIndexValue(host.Status.MachineRef); value != "" {
		return []string{value}
	}
	return nil
}

func machineRefMatches(ref *corev1.ObjectReference, machine *infrastructurev1alpha1.TartMachine) bool {
	if ref == nil {
		return false
	}
	if ref.Name != machine.Name || ref.Namespace != machine.Namespace {
		return false
	}
	return ref.UID == "" || machine.UID == "" || ref.UID == machine.UID
}
