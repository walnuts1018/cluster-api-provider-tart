package machine

import (
	"fmt"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
)

func BeginProvisioningStatus(machine *infrastructurev1alpha1.TartMachine, host *infrastructurev1alpha1.TartHost, token string, now time.Time, ttl time.Duration) infrastructurev1alpha1.TartMachineStatus {
	status := machine.Status.DeepCopy()
	startedAt := metav1.NewTime(now)
	expiresAt := metav1.NewTime(now.Add(ttl))
	status.Ready = false
	status.HostRef = hostdomain.RefForHost(host)
	status.BootstrapToken = token
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
	return *status
}

func RetryExpiredTokenStatus(machine *infrastructurev1alpha1.TartMachine, token string, now time.Time, ttl time.Duration) infrastructurev1alpha1.TartMachineStatus {
	status := machine.Status.DeepCopy()
	startedAt := metav1.NewTime(now)
	expiresAt := metav1.NewTime(now.Add(ttl))
	status.Ready = false
	status.BootstrapToken = token
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
	return *status
}

func ReadyStatus(machine *infrastructurev1alpha1.TartMachine) infrastructurev1alpha1.TartMachineStatus {
	status := machine.Status.DeepCopy()
	status.Ready = true
	status.BootstrapToken = ""
	status.ProvisioningStartTime = nil
	status.TokenExpiresAt = nil
	status.ObservedGeneration = machine.Generation
	return *status
}

func BootstrapTokenConsumedStatus(machine *infrastructurev1alpha1.TartMachine) infrastructurev1alpha1.TartMachineStatus {
	status := machine.Status.DeepCopy()
	status.BootstrapToken = ""
	status.TokenExpiresAt = nil
	status.ObservedGeneration = machine.Generation
	return *status
}

func TokenExpired(machine *infrastructurev1alpha1.TartMachine, now time.Time) bool {
	return machine.Status.TokenExpiresAt != nil && now.After(machine.Status.TokenExpiresAt.Time)
}
