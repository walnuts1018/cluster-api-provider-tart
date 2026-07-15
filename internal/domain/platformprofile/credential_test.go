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

package platformprofile

import "testing"

func TestRequirementForProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		profile string
		want    Requirement
		wantOK  bool
	}{
		{
			name:    "隔離L2必須profile",
			profile: "amd64-uefi-ab/v1",
			want: Requirement{
				Mode:               CredentialModeIsolatedL2,
				IsolatedL2Required: true,
			},
			wantOK: true,
		},
		{
			name:    "未知profile",
			profile: "amd64-uefi-ab/v2",
			wantOK:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := RequirementForProfile(tt.profile)
			if ok != tt.wantOK {
				t.Fatalf("ok = %t, want %t", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("Requirement = %#v, want %#v", got, tt.want)
			}
		})
	}
}
