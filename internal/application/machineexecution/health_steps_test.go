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

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
	machinelifecycledomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinelifecycle"
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
			wantRoute: healthGateUpdateRoute{},
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
			wantRoute: healthGateUpdateTerminalRoute{
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
			wantRoute: healthGateNodeStatusRoute{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route, err := decideProvisionedHealthGateRouteStep(tt.operation.DeepCopy(), machinehealthdomain.NodeObservation{})
			if err != nil {
				t.Fatalf("decideProvisionedHealthGateRouteStep() error = %v", err)
			}

			switch want := tt.wantRoute.(type) {
			case healthGateUpdateRoute:
				if _, ok := route.(healthGateUpdateRoute); !ok {
					t.Fatalf("route = %T, want %T", route, want)
				}
			case healthGateNodeStatusRoute:
				if _, ok := route.(healthGateNodeStatusRoute); !ok {
					t.Fatalf("route = %T, want %T", route, want)
				}
			case healthGateUpdateTerminalRoute:
				got, ok := route.(healthGateUpdateTerminalRoute)
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
			want: provisionHealthGateComplete{},
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
			want: provisionHealthGatePending{Reason: "WaitingForBootReport"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decideProvisionHealthGateStep(tt.operation.DeepCopy(), tt.observation)
			if err != nil {
				t.Fatalf("decideProvisionHealthGateStep() error = %v", err)
			}
			switch want := tt.want.(type) {
			case provisionHealthGateComplete:
				if _, ok := got.(provisionHealthGateComplete); !ok {
					t.Fatalf("decision = %T, want %T", got, want)
				}
			case provisionHealthGatePending:
				pending, ok := got.(provisionHealthGatePending)
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
			want: updateHealthGateComplete{},
		},
		{
			name: "node healthが異常ならRollbackへ進む",
			observation: machinehealthdomain.NodeObservation{
				MachineProviderID: "tart://host-a",
				NodeProviderID:    "tart://host-b",
				NodeReady:         true,
			},
			want: updateHealthGateRollback{},
		},
	}

	operation := &infrastructurev1beta1.TartHostOperation{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decideUpdateHealthGateStep(operation, tt.observation)
			if err != nil {
				t.Fatalf("decideUpdateHealthGateStep() error = %v", err)
			}
			switch want := tt.want.(type) {
			case updateHealthGateComplete:
				if _, ok := got.(updateHealthGateComplete); !ok {
					t.Fatalf("decision = %T, want %T", got, want)
				}
			case updateHealthGateRollback:
				if _, ok := got.(updateHealthGateRollback); !ok {
					t.Fatalf("decision = %T, want %T", got, want)
				}
			default:
				t.Fatalf("unexpected want = %T", tt.want)
			}
		})
	}
}
