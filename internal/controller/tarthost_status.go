package controller

import (
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
)

const tartHostMachineRefField = "status.machineRef"

func tartHostMachineRefIndexValue(ref *corev1.ObjectReference) string {
	return hostdomain.MachineRefIndexValue(ref)
}

func tartHostMachineRefIndexValueForMachine(machine *infrastructurev1alpha1.TartMachine) string {
	return hostdomain.MachineRefIndexValueForMachine(machine)
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
	return hostdomain.MachineRefMatches(ref, machine)
}
