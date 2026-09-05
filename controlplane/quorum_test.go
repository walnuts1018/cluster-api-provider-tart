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

package controlplane

import "testing"

func TestCanRemoveMember(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		observation RemovalObservation
		want        bool
	}{
		"removes healthy member while retaining quorum": {
			observation: RemovalObservation{MemberCount: 3, HealthyMemberCount: 3, TargetHealthy: true, TargetHealthObserved: true},
			want:        true,
		},
		"removes known unhealthy member while retaining quorum": {
			observation: RemovalObservation{MemberCount: 3, HealthyMemberCount: 2, TargetHealthObserved: true},
			want:        true,
		},
		"rejects healthy member removal when remaining set loses quorum": {
			observation: RemovalObservation{MemberCount: 3, HealthyMemberCount: 2, TargetHealthy: true, TargetHealthObserved: true},
			want:        false,
		},
		"rejects unhealthy current cluster": {
			observation: RemovalObservation{MemberCount: 3, HealthyMemberCount: 1, TargetHealthObserved: true},
			want:        false,
		},
		"rejects unknown target health": {
			observation: RemovalObservation{MemberCount: 3, HealthyMemberCount: 3},
			want:        false,
		},
		"rejects removal of the last member": {
			observation: RemovalObservation{MemberCount: 1, HealthyMemberCount: 1, TargetHealthy: true, TargetHealthObserved: true},
			want:        false,
		},
		"rejects inconsistent healthy count": {
			observation: RemovalObservation{MemberCount: 3, HealthyMemberCount: 3, TargetHealthObserved: true},
			want:        false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := CanRemoveMember(tt.observation); got != tt.want {
				t.Errorf("CanRemoveMember() = %t, want %t", got, tt.want)
			}
		})
	}
}
