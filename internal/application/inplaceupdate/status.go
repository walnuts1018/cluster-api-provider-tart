// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package inplaceupdate

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

const ConditionReady = "Ready"

func StatusWithUpdateSucceeded(
	machine *infrastructurev1beta1.TartMachine,
	operation *infrastructurev1beta1.TartHostOperation,
) infrastructurev1beta1.TartMachineStatus {
	status := machine.Status.DeepCopy()
	status.ObservedGeneration = machine.Generation
	status.ActiveSlot = operation.Spec.TargetSlot
	status.InstalledImageDigest = operation.Spec.TargetImageDigest
	status.InstalledDistributionVersion = operation.Spec.TargetDistributionVersion
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Updated",
		Message:            "In-place OS update has completed successfully",
		ObservedGeneration: machine.Generation,
	})
	return *status
}

func StatusWithUpdateRolledBack(
	machine *infrastructurev1beta1.TartMachine,
) infrastructurev1beta1.TartMachineStatus {
	status := machine.Status.DeepCopy()
	status.ObservedGeneration = machine.Generation
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "UpdateRolledBack",
		Message:            "In-place OS update failed and the previous slot is healthy",
		ObservedGeneration: machine.Generation,
	})
	return *status
}

func StatusWithUpdateRecoveryRequired(
	machine *infrastructurev1beta1.TartMachine,
) infrastructurev1beta1.TartMachineStatus {
	status := machine.Status.DeepCopy()
	status.ObservedGeneration = machine.Generation
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "RecoveryRequired",
		Message:            "In-place OS update failed and automatic rollback did not restore health",
		ObservedGeneration: machine.Generation,
	})
	return *status
}
