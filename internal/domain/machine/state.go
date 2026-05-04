package machine

import (
	"errors"
	"fmt"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
)

var ErrIllegalMachineState = errors.New("illegal TartMachine state")

func BeginProvisioningStatus(machine *infrastructurev1alpha1.TartMachine, host *infrastructurev1alpha1.TartHost, now time.Time, ttl time.Duration) (infrastructurev1alpha1.TartMachineStatus, error) {
	if err := validateUnassignedMachineStatus(machine.Status); err != nil {
		return infrastructurev1alpha1.TartMachineStatus{}, err
	}

	status := machine.Status.DeepCopy()
	startedAt := metav1.NewTime(now)
	expiresAt := metav1.NewTime(now.Add(ttl))
	status.Ready = false
	status.HostRef = hostdomain.RefForHost(host)
	status.ProvisioningStartTime = &startedAt
	status.TokenExpiresAt = &expiresAt
	status.ObservedGeneration = machine.Generation
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               "HostReserved",
		Status:             metav1.ConditionTrue,
		Reason:             "ProvisioningStarted",
		Message:            fmt.Sprintf("Reserved TartHost %s/%s and sent Wake-on-LAN", host.Namespace, host.Name),
		ObservedGeneration: machine.Generation,
	})
	return *status, nil
}

func RetryExpiredTokenStatus(machine *infrastructurev1alpha1.TartMachine, now time.Time, ttl time.Duration) (infrastructurev1alpha1.TartMachineStatus, error) {
	if err := validateProvisioningMachineStatus(machine.Status); err != nil {
		return infrastructurev1alpha1.TartMachineStatus{}, err
	}
	if !TokenExpired(machine, now) {
		return infrastructurev1alpha1.TartMachineStatus{}, ErrIllegalMachineState
	}

	status := machine.Status.DeepCopy()
	startedAt := metav1.NewTime(now)
	expiresAt := metav1.NewTime(now.Add(ttl))
	status.Ready = false
	status.ProvisioningStartTime = &startedAt
	status.TokenExpiresAt = &expiresAt
	status.ObservedGeneration = machine.Generation
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               "Provisioning",
		Status:             metav1.ConditionFalse,
		Reason:             "TokenExpired",
		Message:            "Bootstrap token expired, regenerating and retrying",
		ObservedGeneration: machine.Generation,
	})
	return *status, nil
}

func ReadyStatus(machine *infrastructurev1alpha1.TartMachine) (infrastructurev1alpha1.TartMachineStatus, error) {
	if err := validateTokenConsumedMachineStatus(machine.Status); err != nil {
		return infrastructurev1alpha1.TartMachineStatus{}, err
	}

	status := machine.Status.DeepCopy()
	status.Ready = true
	status.Initialization.Provisioned = true
	status.ProvisioningStartTime = nil
	status.ObservedGeneration = machine.Generation
	return *status, nil
}

func BootstrapTokenConsumedStatus(machine *infrastructurev1alpha1.TartMachine) (infrastructurev1alpha1.TartMachineStatus, error) {
	if err := validateProvisioningMachineStatus(machine.Status); err != nil {
		return infrastructurev1alpha1.TartMachineStatus{}, err
	}

	status := machine.Status.DeepCopy()
	status.TokenExpiresAt = nil
	status.ObservedGeneration = machine.Generation
	return *status, nil
}

func TokenExpired(machine *infrastructurev1alpha1.TartMachine, now time.Time) bool {
	return machine.Status.TokenExpiresAt != nil && now.After(machine.Status.TokenExpiresAt.Time)
}

func validateUnassignedMachineStatus(status infrastructurev1alpha1.TartMachineStatus) error {
	if status.Ready || status.HostRef != nil || status.ProvisioningStartTime != nil || status.TokenExpiresAt != nil {
		return ErrIllegalMachineState
	}
	return nil
}

func validateProvisioningMachineStatus(status infrastructurev1alpha1.TartMachineStatus) error {
	if status.Ready || status.HostRef == nil || status.TokenExpiresAt == nil {
		return ErrIllegalMachineState
	}
	return nil
}

func validateTokenConsumedMachineStatus(status infrastructurev1alpha1.TartMachineStatus) error {
	if status.Ready || status.HostRef == nil || status.TokenExpiresAt != nil {
		return ErrIllegalMachineState
	}
	return nil
}
