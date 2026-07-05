package initialprovisioning

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

const (
	// ConditionReady はCAPIが要求するInfrastructure Ready条件の型。
	ConditionReady = "Ready"
	// ConditionProvisioning はプロビジョニング中を示す条件の型。
	ConditionProvisioning = "Provisioning"
)

// StatusWithHostReserved はHostRefとOperationRefをStatusに反映した新しいStatusを返す。
func StatusWithHostReserved(
	machine *infrastructurev1beta1.TartMachine,
	host *infrastructurev1beta1.TartHost,
	operation *infrastructurev1beta1.TartHostOperation,
) infrastructurev1beta1.TartMachineStatus {
	status := machine.Status.DeepCopy()
	status.HostRef = &infrastructurev1beta1.ResourceReference{
		Namespace: host.Namespace,
		Name:      host.Name,
		UID:       host.UID,
	}
	status.OperationRef = &infrastructurev1beta1.ResourceReference{
		Namespace: operation.Namespace,
		Name:      operation.Name,
		UID:       operation.UID,
	}
	status.ObservedGeneration = machine.Generation
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               ConditionProvisioning,
		Status:             metav1.ConditionTrue,
		Reason:             "HostReserved",
		Message:            "TartHost has been reserved and provisioning is in progress",
		ObservedGeneration: machine.Generation,
	})
	return *status
}

// StatusWithProvisioned はプロビジョニング完了時のStatusを返す。
// ProviderIDをSpec.ProviderIDとして書き込む呼び出し元のPatchを補助し、
// Addressを設定し、Ready条件をTrueにする。
func StatusWithProvisioned(
	machine *infrastructurev1beta1.TartMachine,
	addresses []clusterv1.MachineAddress,
) infrastructurev1beta1.TartMachineStatus {
	status := machine.Status.DeepCopy()
	status.ObservedGeneration = machine.Generation

	if len(addresses) > 0 {
		status.Addresses = addresses
	}

	provisioned := true
	status.Initialization = infrastructurev1beta1.TartMachineInitializationStatus{
		Provisioned: &provisioned,
	}

	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Provisioned",
		Message:            "TartMachine has been provisioned and the Node is ready",
		ObservedGeneration: machine.Generation,
	})
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               ConditionProvisioning,
		Status:             metav1.ConditionFalse,
		Reason:             "Provisioned",
		Message:            "Provisioning has completed successfully",
		ObservedGeneration: machine.Generation,
	})
	return *status
}

// StatusWithProvisionFailed はプロビジョニング失敗時のStatusを返す。
func StatusWithProvisionFailed(
	machine *infrastructurev1beta1.TartMachine,
	reason, message string,
) infrastructurev1beta1.TartMachineStatus {
	status := machine.Status.DeepCopy()
	status.ObservedGeneration = machine.Generation
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: machine.Generation,
	})
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               ConditionProvisioning,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: machine.Generation,
	})
	return *status
}

// StatusWithWaitingForBootstrap はBootstrapが未準備の場合のStatusを返す。
func StatusWithWaitingForBootstrap(machine *infrastructurev1beta1.TartMachine) infrastructurev1beta1.TartMachineStatus {
	status := machine.Status.DeepCopy()
	status.ObservedGeneration = machine.Generation
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               ConditionProvisioning,
		Status:             metav1.ConditionFalse,
		Reason:             "WaitingForBootstrap",
		Message:            "Waiting for bootstrap data to be available",
		ObservedGeneration: machine.Generation,
	})
	return *status
}

// StatusWithNoAvailableHost はAvailableなHostが存在しない場合のStatusを返す。
func StatusWithNoAvailableHost(machine *infrastructurev1beta1.TartMachine) infrastructurev1beta1.TartMachineStatus {
	status := machine.Status.DeepCopy()
	status.ObservedGeneration = machine.Generation
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               ConditionProvisioning,
		Status:             metav1.ConditionFalse,
		Reason:             "NoAvailableHost",
		Message:            "No available TartHost matches the requirements; will retry",
		ObservedGeneration: machine.Generation,
	})
	return *status
}

// StatusWithHealthGatePending は初期Provisioningの完了条件が不足しているStatusを返す。
func StatusWithHealthGatePending(
	machine *infrastructurev1beta1.TartMachine,
	reason, message string,
) infrastructurev1beta1.TartMachineStatus {
	status := machine.Status.DeepCopy()
	status.ObservedGeneration = machine.Generation
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: machine.Generation,
	})
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               ConditionProvisioning,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: machine.Generation,
	})
	return *status
}
