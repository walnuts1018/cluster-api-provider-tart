package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// OwnershipFailureはprovider resourceの所有権検証に失敗したことを表す。reason/messageは
// 呼び出し元(tartcontrolplaneのcontrolPlaneFailureなど)がConditionへ変換できるよう保持する。
type OwnershipFailure struct {
	Reason  string
	Message string
}

func (f *OwnershipFailure) Error() string {
	return f.Reason + ": " + f.Message
}

// ValidateProviderOwnerは、providerリソース(TartMachine、TartBootstrapConfigなど)がCAPI Machineから
// controller ownerとして正しく所有されていることを検証する。tartcontrolplaneとtartbootstrapconfigの
// 両方から、providerリソースがCAPI Machineの意図しない書き換えを受けていないことの確認に使う。
func ValidateProviderOwner(object metav1.Object, machine *clusterv1.Machine, apiVersion, kind string) error {
	if len(object.GetOwnerReferences()) != 1 || !HasControllerOwner(object, machine, apiVersion, kind) {
		return &OwnershipFailure{Reason: "MachineOwnershipMismatch", Message: "A provider resource is not owned by its corresponding CAPI Machine."}
	}
	return nil
}

// HasControllerOwnerは、objectがownerをcontroller ownerReferenceとして参照しているかを判定する。
func HasControllerOwner(object metav1.Object, owner metav1.Object, apiVersion, kind string) bool {
	for _, reference := range object.GetOwnerReferences() {
		if reference.APIVersion == apiVersion && reference.Kind == kind && reference.Name == owner.GetName() && reference.UID == owner.GetUID() && reference.Controller != nil && *reference.Controller {
			return true
		}
	}
	return false
}
