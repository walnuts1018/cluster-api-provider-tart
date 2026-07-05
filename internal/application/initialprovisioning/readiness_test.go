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
	"testing"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
)

func TestEvaluateReadinessRequiresEveryProvisioningGate(t *testing.T) {
	t.Parallel()

	readyOperation := infrastructurev1beta1.TartHostOperation{
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			Type: infrastructurev1beta1.OperationTypeProvision,
		},
		Status: infrastructurev1beta1.TartHostOperationStatus{
			Phase: infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
			LastBootReport: &infrastructurev1beta1.BootReportStatus{
				StateMounted:     true,
				DataMounted:      true,
				BootstrapApplied: true,
			},
		},
	}
	readyNode := machinehealthdomain.NodeObservation{
		MachineProviderID: "tart://host-a",
		NodeProviderID:    "tart://host-a",
		NodeReady:         true,
		ExpectedVersion:   "v1.35.0",
		NodeVersion:       "v1.35.0",
	}

	tests := []struct {
		name   string
		mutate func(*infrastructurev1beta1.TartHostOperation, *machinehealthdomain.NodeObservation)
		reason string
	}{
		{
			name: "AwaitingHealth前",
			mutate: func(operation *infrastructurev1beta1.TartHostOperation, _ *machinehealthdomain.NodeObservation) {
				operation.Status.Phase = infrastructurev1beta1.TartHostOperationPhaseBootTrial
			},
			reason: "WaitingForBoot",
		},
		{
			name: "boot reportなし",
			mutate: func(operation *infrastructurev1beta1.TartHostOperation, _ *machinehealthdomain.NodeObservation) {
				operation.Status.LastBootReport = nil
			},
			reason: "WaitingForBootReport",
		},
		{
			name: "State未mount",
			mutate: func(operation *infrastructurev1beta1.TartHostOperation, _ *machinehealthdomain.NodeObservation) {
				operation.Status.LastBootReport.StateMounted = false
			},
			reason: "StateNotMounted",
		},
		{
			name: "Data未mount",
			mutate: func(operation *infrastructurev1beta1.TartHostOperation, _ *machinehealthdomain.NodeObservation) {
				operation.Status.LastBootReport.DataMounted = false
			},
			reason: "DataNotMounted",
		},
		{
			name: "Bootstrap未適用",
			mutate: func(operation *infrastructurev1beta1.TartHostOperation, _ *machinehealthdomain.NodeObservation) {
				operation.Status.LastBootReport.BootstrapApplied = false
			},
			reason: "BootstrapNotApplied",
		},
		{
			name: "providerID不一致",
			mutate: func(_ *infrastructurev1beta1.TartHostOperation, node *machinehealthdomain.NodeObservation) {
				node.NodeProviderID = "tart://host-b"
			},
			reason: "ProviderIDMismatch",
		},
		{
			name: "Kubernetes version不一致",
			mutate: func(_ *infrastructurev1beta1.TartHostOperation, node *machinehealthdomain.NodeObservation) {
				node.NodeVersion = "v1.34.0"
			},
			reason: "KubernetesVersionMismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operation := readyOperation.DeepCopy()
			node := readyNode
			tt.mutate(operation, &node)

			got := EvaluateReadiness(operation, node)
			if got.Ready {
				t.Fatal("Ready = true, want false")
			}
			if got.Reason != tt.reason {
				t.Fatalf("Reason = %q, want %q", got.Reason, tt.reason)
			}
		})
	}

	got := EvaluateReadiness(&readyOperation, readyNode)
	if !got.Ready || got.Reason != "Provisioned" {
		t.Fatalf("EvaluateReadiness() = %#v, want Provisioned", got)
	}
}
