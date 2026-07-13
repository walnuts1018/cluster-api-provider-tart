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

import "testing"

func TestDecide(t *testing.T) {
	tests := map[string]struct {
		tartCluster TartCluster
		capiCluster CAPICluster
		want        Decision
	}{
		"クラスタラベルがなければstatusを変更しない": {
			tartCluster: TartCluster{Generation: 7},
			capiCluster: MissingClusterLabel{},
			want:        DecisionSkipMissingClusterLabel{},
		},
		"CAPI Clusterがなければstatusを変更しない": {
			tartCluster: TartCluster{Generation: 7},
			capiCluster: ClusterNotFound{Name: "owned-cluster"},
			want:        DecisionSkipClusterNotFound{ClusterName: "owned-cluster"},
		},
		"CAPI Clusterがpausedならstatusを変更しない": {
			tartCluster: TartCluster{Generation: 7},
			capiCluster: PausedCluster{Name: "owned-cluster"},
			want:        DecisionSkipPausedCluster{ClusterName: "owned-cluster"},
		},
		"control planeが未readyならprovisionedとnot ready条件を計画する": {
			tartCluster: TartCluster{Generation: 7, ControlPlaneReady: false},
			capiCluster: ActiveCluster{Name: "owned-cluster"},
			want: DecisionApplyStatus{Plan: StatusPlan{
				Generation:               7,
				Provisioned:              true,
				ControlPlaneReady:        false,
				MarkControlPlaneNotReady: true,
			}},
		},
		"control planeがreadyならready条件を計画する": {
			tartCluster: TartCluster{Generation: 7, ControlPlaneReady: true},
			capiCluster: ActiveCluster{Name: "owned-cluster"},
			want: DecisionApplyStatus{Plan: StatusPlan{
				Generation:               7,
				Provisioned:              true,
				ControlPlaneReady:        true,
				MarkControlPlaneNotReady: false,
			}},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := Decide(tt.tartCluster, tt.capiCluster)
			if err != nil {
				t.Fatalf("Decide() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("Decide() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestObservePause(t *testing.T) {
	tests := map[string]struct {
		observation PauseObservation
		wantPaused  bool
	}{
		"spec paused": {
			observation: PauseObservation{SpecPaused: true},
			wantPaused:  true,
		},
		"paused annotation": {
			observation: PauseObservation{PausedAnnotated: true},
			wantPaused:  true,
		},
		"not paused": {
			observation: PauseObservation{},
			wantPaused:  false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, gotPaused := ObservePause(tt.observation).(PausedCluster)
			if gotPaused != tt.wantPaused {
				t.Fatalf("ObservePause() paused = %t, want %t", gotPaused, tt.wantPaused)
			}
		})
	}
}
