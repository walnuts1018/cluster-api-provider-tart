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

package machinehealth

import "testing"

func TestEvaluateNode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		observation NodeObservation
		wantReady   bool
		wantReason  Reason
	}{
		{
			name: "一致してReady",
			observation: NodeObservation{
				MachineProviderID: "tart://host-a",
				NodeProviderID:    "tart://host-a",
				NodeReady:         true,
				ExpectedMachineID: "machine-id",
				ObservedMachineID: "machine-id",
				ExpectedVersion:   "v1.36.0",
				NodeVersion:       "v1.36.0",
			},
			wantReady:  true,
			wantReason: ReasonProvisioned,
		},
		{
			name: "Kubernetes version不一致",
			observation: NodeObservation{
				MachineProviderID: "tart://host-a",
				NodeProviderID:    "tart://host-a",
				NodeReady:         true,
				ExpectedVersion:   "v1.36.0",
				NodeVersion:       "v1.34.0",
			},
			wantReason: ReasonKubernetesVersionMismatch,
		},
		{
			name: "providerID不一致",
			observation: NodeObservation{
				MachineProviderID: "tart://host-a",
				NodeProviderID:    "tart://host-b",
				NodeReady:         true,
			},
			wantReason: ReasonProviderIDMismatch,
		},
		{
			name: "NodeがReadyでない",
			observation: NodeObservation{
				MachineProviderID: "tart://host-a",
				NodeProviderID:    "tart://host-a",
			},
			wantReason: ReasonNodeNotReady,
		},
		{
			name: "machine-id不一致",
			observation: NodeObservation{
				MachineProviderID: "tart://host-a",
				NodeProviderID:    "tart://host-a",
				NodeReady:         true,
				ExpectedMachineID: "machine-id-a",
				ObservedMachineID: "machine-id-b",
			},
			wantReason: ReasonMachineIDMismatch,
		},
		{
			name: "MachineのproviderIDがない",
			observation: NodeObservation{
				NodeProviderID: "tart://host-a",
				NodeReady:      true,
			},
			wantReason: ReasonProviderIDMissing,
		},
		{
			name: "NodeのproviderIDがない",
			observation: NodeObservation{
				MachineProviderID: "tart://host-a",
				NodeReady:         true,
			},
			wantReason: ReasonProviderIDMissing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := EvaluateNode(tt.observation)
			if got.Ready != tt.wantReady {
				t.Fatalf("Ready = %t, want %t", got.Ready, tt.wantReady)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("Reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}
