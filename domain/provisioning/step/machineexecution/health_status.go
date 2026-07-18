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

package step

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/machinehealth"
)

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
