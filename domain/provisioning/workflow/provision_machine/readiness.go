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

package initialprovisioning

import (
	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/machinehealth"
)

// ReadinessResult は初期Provisioningの完了判定を表す。
type ReadinessResult struct {
	Ready   bool
	Reason  string
	Message string
}

// EvaluateReadiness はOperation、OS boot、Bootstrap、Nodeの観測結果をまとめて判定する。
// Agentの書き込み完了やNode ReadyだけをProvisioning完了として扱わない。
func EvaluateReadiness(
	operation *infrastructurev1beta1.TartHostOperation,
	node machinehealthdomain.NodeObservation,
) ReadinessResult {
	if operation == nil || operation.Spec.Type != infrastructurev1beta1.OperationTypeProvision {
		return ReadinessResult{Reason: "InvalidOperation", Message: "A Provision operation is required"}
	}
	if operation.Status.Phase != infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth &&
		operation.Status.Phase != infrastructurev1beta1.TartHostOperationPhaseSucceeded {
		return ReadinessResult{Reason: "WaitingForBoot", Message: "Provision operation has not reached the health gate"}
	}
	report := operation.Status.LastBootReport
	if report == nil {
		return ReadinessResult{Reason: "WaitingForBootReport", Message: "Waiting for an authenticated OS boot report"}
	}
	if !report.StateMounted {
		return ReadinessResult{Reason: "StateNotMounted", Message: "The required State filesystem is not mounted"}
	}
	if !report.DataMounted {
		return ReadinessResult{Reason: "DataNotMounted", Message: "The required Data filesystem is not mounted"}
	}
	if !report.BootstrapApplied {
		return ReadinessResult{Reason: "BootstrapNotApplied", Message: "Bootstrap success marker has not been reported"}
	}
	if report.BootstrapPayloadDigest == "" {
		return ReadinessResult{Reason: "BootstrapPayloadDigestMissing", Message: "Bootstrap success marker payload digest has not been reported"}
	}

	health := machinehealthdomain.EvaluateNode(node)
	if !health.Ready {
		return ReadinessResult{Reason: string(health.Reason), Message: health.Message}
	}
	return ReadinessResult{
		Ready:   true,
		Reason:  "Provisioned",
		Message: "OS boot, Bootstrap application, and Node health gates are complete",
	}
}
