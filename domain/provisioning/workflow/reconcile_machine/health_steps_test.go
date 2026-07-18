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

package machineexecution

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/entity/machinelifecycle"
	machineexecutionstep "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/step/machineexecution"
	model "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/step/machineexecution/model"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/machinehealth"
)

func TestDecideProvisionedHealthGateRouteStep(t *testing.T) {
	tests := []struct {
		name      string
		operation infrastructurev1beta1.TartHostOperation
		wantRoute any
	}{
		{
			name: "update awaiting health continues through update health gate",
			operation: infrastructurev1beta1.TartHostOperation{
				Spec: infrastructurev1beta1.TartHostOperationSpec{
					Type: infrastructurev1beta1.OperationTypeUpdate,
				},
				Status: infrastructurev1beta1.TartHostOperationStatus{
					Phase: infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
				},
			},
			wantRoute: model.HealthGateUpdateRoute{},
		},
		{
			name: "update terminal phase is routed to terminal application",
			operation: infrastructurev1beta1.TartHostOperation{
				Spec: infrastructurev1beta1.TartHostOperationSpec{
					Type: infrastructurev1beta1.OperationTypeUpdate,
				},
				Status: infrastructurev1beta1.TartHostOperationStatus{
					Phase: infrastructurev1beta1.TartHostOperationPhaseSucceeded,
				},
			},
			wantRoute: model.HealthGateUpdateTerminalRoute{
				Outcome: machinelifecycledomain.UpdateOutcomeSucceeded,
			},
		},
		{
			name: "non-update operation only refreshes node health status",
			operation: infrastructurev1beta1.TartHostOperation{
				Spec: infrastructurev1beta1.TartHostOperationSpec{
					Type: infrastructurev1beta1.OperationTypeProvision,
				},
				Status: infrastructurev1beta1.TartHostOperationStatus{
					Phase: infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
				},
			},
			wantRoute: model.HealthGateNodeStatusRoute{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route, err := machineexecutionstep.DecideProvisionedHealthGateRoute(tt.operation.DeepCopy(), machinehealthdomain.NodeObservation{})
			if err != nil {
				t.Fatalf("DecideProvisionedHealthGateRoute() error = %v", err)
			}

			switch want := tt.wantRoute.(type) {
			case model.HealthGateUpdateRoute:
				if _, ok := route.(model.HealthGateUpdateRoute); !ok {
					t.Fatalf("route = %T, want %T", route, want)
				}
			case model.HealthGateNodeStatusRoute:
				if _, ok := route.(model.HealthGateNodeStatusRoute); !ok {
					t.Fatalf("route = %T, want %T", route, want)
				}
			case model.HealthGateUpdateTerminalRoute:
				got, ok := route.(model.HealthGateUpdateTerminalRoute)
				if !ok {
					t.Fatalf("route = %T, want %T", route, want)
				}
				if got.Outcome != want.Outcome {
					t.Fatalf("Outcome = %q, want %q", got.Outcome, want.Outcome)
				}
			default:
				t.Fatalf("unexpected wantRoute = %T", tt.wantRoute)
			}
		})
	}
}

func TestDecideProvisionHealthGateStep(t *testing.T) {
	tests := []struct {
		name        string
		operation   infrastructurev1beta1.TartHostOperation
		observation machinehealthdomain.NodeObservation
		want        any
	}{
		{
			name: "bootとbootstrapとnode healthが揃うとProvision完了へ進む",
			operation: infrastructurev1beta1.TartHostOperation{
				Spec: infrastructurev1beta1.TartHostOperationSpec{
					Type: infrastructurev1beta1.OperationTypeProvision,
				},
				Status: infrastructurev1beta1.TartHostOperationStatus{
					Phase: infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
					LastBootReport: &infrastructurev1beta1.BootReportStatus{
						StateMounted:           true,
						DataMounted:            true,
						BootstrapApplied:       true,
						BootstrapPayloadDigest: "sha256:" + strings.Repeat("d", 64),
					},
				},
			},
			observation: machinehealthdomain.NodeObservation{
				MachineProviderID: "tart://host-a",
				NodeProviderID:    "tart://host-a",
				NodeReady:         true,
			},
			want: model.ProvisionHealthGateComplete{},
		},
		{
			name: "boot reportが未到達ならHealth Gate保留にする",
			operation: infrastructurev1beta1.TartHostOperation{
				Spec: infrastructurev1beta1.TartHostOperationSpec{
					Type: infrastructurev1beta1.OperationTypeProvision,
				},
				Status: infrastructurev1beta1.TartHostOperationStatus{
					Phase: infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
				},
			},
			observation: machinehealthdomain.NodeObservation{
				MachineProviderID: "tart://host-a",
				NodeProviderID:    "tart://host-a",
				NodeReady:         true,
			},
			want: model.ProvisionHealthGatePending{Reason: "WaitingForBootReport"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := machineexecutionstep.DecideProvisionHealthGate(tt.operation.DeepCopy(), tt.observation)
			if err != nil {
				t.Fatalf("DecideProvisionHealthGate() error = %v", err)
			}
			switch want := tt.want.(type) {
			case model.ProvisionHealthGateComplete:
				if _, ok := got.(model.ProvisionHealthGateComplete); !ok {
					t.Fatalf("decision = %T, want %T", got, want)
				}
			case model.ProvisionHealthGatePending:
				pending, ok := got.(model.ProvisionHealthGatePending)
				if !ok {
					t.Fatalf("decision = %T, want %T", got, want)
				}
				if pending.Reason != want.Reason {
					t.Fatalf("Reason = %q, want %q", pending.Reason, want.Reason)
				}
			default:
				t.Fatalf("unexpected want = %T", tt.want)
			}
		})
	}
}

func TestDecideProvisionProgressStep(t *testing.T) {
	tests := []struct {
		name      string
		operation infrastructurev1beta1.TartHostOperation
		want      any
	}{
		{
			name: "provision operationがfailedなら失敗statusへ進む",
			operation: infrastructurev1beta1.TartHostOperation{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "operation-a",
				},
				Spec: infrastructurev1beta1.TartHostOperationSpec{
					Type: infrastructurev1beta1.OperationTypeProvision,
				},
				Status: infrastructurev1beta1.TartHostOperationStatus{
					Phase: infrastructurev1beta1.TartHostOperationPhaseFailed,
				},
			},
			want: model.ProvisionProgressFailed{
				Reason:  "OperationFailed",
				Message: "TartHostOperation default/operation-a Failed",
			},
		},
		{
			name: "provision operationがhealth待ちならhealth観測を待つ",
			operation: infrastructurev1beta1.TartHostOperation{
				Spec: infrastructurev1beta1.TartHostOperationSpec{
					Type: infrastructurev1beta1.OperationTypeProvision,
				},
				Status: infrastructurev1beta1.TartHostOperationStatus{
					Phase: infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
				},
			},
			want: model.ProvisionProgressAwaitingHealth{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := machineexecutionstep.DecideProvisionProgress(tt.operation.DeepCopy())
			if err != nil {
				t.Fatalf("DecideProvisionProgress() error = %v", err)
			}
			switch want := tt.want.(type) {
			case model.ProvisionProgressFailed:
				failed, ok := got.(model.ProvisionProgressFailed)
				if !ok {
					t.Fatalf("decision = %T, want %T", got, want)
				}
				if failed.Reason != want.Reason {
					t.Fatalf("Reason = %q, want %q", failed.Reason, want.Reason)
				}
				if failed.Message != want.Message {
					t.Fatalf("Message = %q, want %q", failed.Message, want.Message)
				}
			case model.ProvisionProgressAwaitingHealth:
				if _, ok := got.(model.ProvisionProgressAwaitingHealth); !ok {
					t.Fatalf("decision = %T, want %T", got, want)
				}
			default:
				t.Fatalf("unexpected want = %T", tt.want)
			}
		})
	}
}

func TestDecideUpdateHealthGateStep(t *testing.T) {
	tests := []struct {
		name        string
		observation machinehealthdomain.NodeObservation
		want        any
	}{
		{
			name: "node healthが正常ならUpdate完了へ進む",
			observation: machinehealthdomain.NodeObservation{
				MachineProviderID: "tart://host-a",
				NodeProviderID:    "tart://host-a",
				NodeReady:         true,
			},
			want: model.UpdateHealthGateComplete{},
		},
		{
			name: "node healthが異常ならRollbackへ進む",
			observation: machinehealthdomain.NodeObservation{
				MachineProviderID: "tart://host-a",
				NodeProviderID:    "tart://host-b",
				NodeReady:         true,
			},
			want: model.UpdateHealthGateRollback{},
		},
	}

	operation := &infrastructurev1beta1.TartHostOperation{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := machineexecutionstep.DecideUpdateHealthGate(operation, tt.observation)
			if err != nil {
				t.Fatalf("DecideUpdateHealthGate() error = %v", err)
			}
			switch want := tt.want.(type) {
			case model.UpdateHealthGateComplete:
				if _, ok := got.(model.UpdateHealthGateComplete); !ok {
					t.Fatalf("decision = %T, want %T", got, want)
				}
			case model.UpdateHealthGateRollback:
				if _, ok := got.(model.UpdateHealthGateRollback); !ok {
					t.Fatalf("decision = %T, want %T", got, want)
				}
			default:
				t.Fatalf("unexpected want = %T", tt.want)
			}
		})
	}
}

func TestDecideUpdateOperationStep(t *testing.T) {
	tests := []struct {
		name      string
		operation infrastructurev1beta1.TartHostOperation
		want      any
	}{
		{
			name: "updateがterminal phaseならterminal適用へ進む",
			operation: infrastructurev1beta1.TartHostOperation{
				Spec: infrastructurev1beta1.TartHostOperationSpec{
					Type: infrastructurev1beta1.OperationTypeUpdate,
				},
				Status: infrastructurev1beta1.TartHostOperationStatus{
					Phase: infrastructurev1beta1.TartHostOperationPhaseSucceeded,
				},
			},
			want: model.UpdateOperationApplyTerminal{Outcome: machinelifecycledomain.UpdateOutcomeSucceeded},
		},
		{
			name: "updateがHealth Gate待ちならnode health観測へ戻す",
			operation: infrastructurev1beta1.TartHostOperation{
				Spec: infrastructurev1beta1.TartHostOperationSpec{
					Type: infrastructurev1beta1.OperationTypeUpdate,
				},
				Status: infrastructurev1beta1.TartHostOperationStatus{
					Phase: infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
				},
			},
			want: model.UpdateOperationRouteNodeHealth{},
		},
		{
			name: "provision operationはnode health観測へ戻す",
			operation: infrastructurev1beta1.TartHostOperation{
				Spec: infrastructurev1beta1.TartHostOperationSpec{
					Type: infrastructurev1beta1.OperationTypeProvision,
				},
				Status: infrastructurev1beta1.TartHostOperationStatus{
					Phase: infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
				},
			},
			want: model.UpdateOperationRouteNodeHealth{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := machineexecutionstep.DecideUpdateOperation(tt.operation.DeepCopy())
			if err != nil {
				t.Fatalf("DecideUpdateOperation() error = %v", err)
			}
			switch want := tt.want.(type) {
			case model.UpdateOperationApplyTerminal:
				terminal, ok := got.(model.UpdateOperationApplyTerminal)
				if !ok {
					t.Fatalf("decision = %T, want %T", got, want)
				}
				if terminal.Outcome != want.Outcome {
					t.Fatalf("Outcome = %q, want %q", terminal.Outcome, want.Outcome)
				}
			case model.UpdateOperationRouteNodeHealth:
				if _, ok := got.(model.UpdateOperationRouteNodeHealth); !ok {
					t.Fatalf("decision = %T, want %T", got, want)
				}
			default:
				t.Fatalf("unexpected want = %T", tt.want)
			}
		})
	}
}
