package host

import (
	"errors"
	"fmt"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
)

var (
	ErrIllegalHostState = errors.New("illegal TartHost state")
	ErrHostNotAvailable = errors.New("TartHost is not available")
)

func ReserveStatus(host *infrastructurev1alpha1.TartHost, machine *infrastructurev1alpha1.TartMachine) (infrastructurev1alpha1.TartHostStatus, error) {
	if err := validateHostState(host.Status); err != nil {
		return infrastructurev1alpha1.TartHostStatus{}, err
	}
	if host.Status.State != infrastructurev1alpha1.TartHostStateAvailable {
		return infrastructurev1alpha1.TartHostStatus{}, ErrHostNotAvailable
	}

	status := host.Status.DeepCopy()
	status.State = infrastructurev1alpha1.TartHostStateReserved
	status.MachineRef = RefForMachine(machine)
	status.ObservedGeneration = host.Generation
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             metav1.ConditionFalse,
		Reason:             "Reserved",
		Message:            fmt.Sprintf("Reserved by TartMachine %s/%s", machine.Namespace, machine.Name),
		ObservedGeneration: host.Generation,
	})
	return *status, nil
}

func ProvisioningStatus(host *infrastructurev1alpha1.TartHost) (infrastructurev1alpha1.TartHostStatus, error) {
	if err := validateAssignedHostState(host.Status); err != nil {
		return infrastructurev1alpha1.TartHostStatus{}, err
	}

	status := host.Status.DeepCopy()
	status.State = infrastructurev1alpha1.TartHostStateProvisioning
	status.ObservedGeneration = host.Generation
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             metav1.ConditionFalse,
		Reason:             "Provisioning",
		Message:            "Host is provisioning a TartMachine after Wake-on-LAN",
		ObservedGeneration: host.Generation,
	})
	return *status, nil
}

func ProvisionedStatus(host *infrastructurev1alpha1.TartHost) (infrastructurev1alpha1.TartHostStatus, error) {
	if err := validateAssignedHostState(host.Status); err != nil {
		return infrastructurev1alpha1.TartHostStatus{}, err
	}

	status := host.Status.DeepCopy()
	status.State = infrastructurev1alpha1.TartHostStateProvisioned
	status.ObservedGeneration = host.Generation
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             metav1.ConditionFalse,
		Reason:             "Provisioned",
		Message:            "Host has been provisioned successfully",
		ObservedGeneration: host.Generation,
	})
	return *status, nil
}

func AvailableStatus(host *infrastructurev1alpha1.TartHost, reason, message string) infrastructurev1alpha1.TartHostStatus {
	status := host.Status.DeepCopy()
	status.State = infrastructurev1alpha1.TartHostStateAvailable
	status.MachineRef = nil
	status.ObservedGeneration = host.Generation
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               "Available",
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: host.Generation,
	})
	return *status
}

func IsAvailableForReservation(host *infrastructurev1alpha1.TartHost) bool {
	return validateHostState(host.Status) == nil &&
		host.Status.State == infrastructurev1alpha1.TartHostStateAvailable &&
		host.Status.MachineRef == nil
}

func validateAssignedHostState(status infrastructurev1alpha1.TartHostStatus) error {
	if err := validateHostState(status); err != nil {
		return err
	}
	if status.MachineRef == nil {
		return ErrIllegalHostState
	}
	return nil
}

func validateHostState(status infrastructurev1alpha1.TartHostStatus) error {
	switch status.State {
	case infrastructurev1alpha1.TartHostStateAvailable:
		if status.MachineRef != nil {
			return ErrIllegalHostState
		}
	case infrastructurev1alpha1.TartHostStateReserved,
		infrastructurev1alpha1.TartHostStateProvisioning,
		infrastructurev1alpha1.TartHostStateProvisioned:
		if status.MachineRef == nil {
			return ErrIllegalHostState
		}
	case "":
		if status.MachineRef != nil {
			return ErrIllegalHostState
		}
	default:
		return ErrIllegalHostState
	}
	return nil
}
