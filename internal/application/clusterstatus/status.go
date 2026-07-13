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

package clusterstatus

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	clusterstatusdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/clusterstatus"
)

const (
	ReadyCondition                     = "Ready"
	ControlPlaneReadyCondition         = "ControlPlaneReady"
	InfrastructureProvisionedCondition = "InfrastructureProvisioned"
)

func StatusWithPlan(
	cluster *infrastructurev1beta1.TartCluster,
	plan clusterstatusdomain.StatusPlan,
) infrastructurev1beta1.TartClusterStatus {
	status := *cluster.Status.DeepCopy()
	status.ObservedGeneration = plan.Generation
	status.Initialization.Provisioned = &plan.Provisioned

	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               InfrastructureProvisionedCondition,
		Status:             metav1.ConditionTrue,
		Reason:             "Provisioned",
		Message:            "TartCluster infrastructure is provisioned",
		ObservedGeneration: plan.Generation,
	})

	if plan.MarkControlPlaneNotReady {
		apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
			Type:               ControlPlaneReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             "ControlPlaneNotReady",
			Message:            "Control plane is not ready yet",
			ObservedGeneration: plan.Generation,
		})
	}

	readyStatus := metav1.ConditionFalse
	readyReason := "NotReady"
	readyMessage := "TartCluster is not ready yet"
	if plan.ControlPlaneReady {
		readyStatus = metav1.ConditionTrue
		readyReason = "Ready"
		readyMessage = "TartCluster is ready"
	}
	apimeta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:               ReadyCondition,
		Status:             readyStatus,
		Reason:             readyReason,
		Message:            readyMessage,
		ObservedGeneration: plan.Generation,
	})

	return status
}
