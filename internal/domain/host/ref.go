package host

import (
	corev1 "k8s.io/api/core/v1"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
)

func BootMACAddress(host *infrastructurev1alpha1.TartHost) string {
	if host.Spec.BootMACAddress != "" {
		return host.Spec.BootMACAddress
	}
	return host.Spec.MACAddress
}

func RefForHost(host *infrastructurev1alpha1.TartHost) *corev1.ObjectReference {
	return &corev1.ObjectReference{
		APIVersion: infrastructurev1alpha1.GroupVersion.String(),
		Kind:       "TartHost",
		Namespace:  host.Namespace,
		Name:       host.Name,
		UID:        host.UID,
	}
}

func RefForMachine(machine *infrastructurev1alpha1.TartMachine) *corev1.ObjectReference {
	return &corev1.ObjectReference{
		APIVersion: infrastructurev1alpha1.GroupVersion.String(),
		Kind:       "TartMachine",
		Namespace:  machine.Namespace,
		Name:       machine.Name,
		UID:        machine.UID,
	}
}

func MachineRefMatches(ref *corev1.ObjectReference, machine *infrastructurev1alpha1.TartMachine) bool {
	if ref == nil {
		return false
	}
	if ref.Name != machine.Name || ref.Namespace != machine.Namespace {
		return false
	}
	return ref.UID == "" || machine.UID == "" || ref.UID == machine.UID
}

func MachineRefIndexValue(ref *corev1.ObjectReference) string {
	if ref == nil {
		return ""
	}
	return ref.Namespace + "/" + ref.Name + "/" + string(ref.UID)
}

func MachineRefIndexValueForMachine(machine *infrastructurev1alpha1.TartMachine) string {
	return machine.Namespace + "/" + machine.Name + "/" + string(machine.UID)
}
