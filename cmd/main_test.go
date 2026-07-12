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

package main

import "testing"

func TestResolveUpdateFeatureGates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input map[string]bool
		want  updateFeatureGates
	}{
		{
			name: "親gate無効なら子gateも無効",
			input: map[string]bool{
				"InPlaceUpdatesWorker":             true,
				"InPlaceUpdatesMultiControlPlane":  true,
				"InPlaceUpdatesSingleControlPlane": true,
			},
			want: updateFeatureGates{},
		},
		{
			name: "親gate有効時にworkerだけ有効",
			input: map[string]bool{
				"InPlaceUpdates":       true,
				"InPlaceUpdatesWorker": true,
			},
			want: updateFeatureGates{
				InPlaceUpdates: true,
				Worker:         true,
			},
		},
		{
			name: "InPlaceUpdatesはworkerから順に有効化する",
			input: map[string]bool{
				"InPlaceUpdates":                   true,
				"InPlaceUpdatesWorker":             true,
				"InPlaceUpdatesMultiControlPlane":  true,
				"InPlaceUpdatesSingleControlPlane": true,
			},
			want: updateFeatureGates{
				InPlaceUpdates:     true,
				Worker:             true,
				MultiControlPlane:  true,
				SingleControlPlane: true,
			},
		},
		{
			name: "InPlaceUpdatesは前段階なしに後段階を有効化しない",
			input: map[string]bool{
				"InPlaceUpdates":                   true,
				"InPlaceUpdatesMultiControlPlane":  true,
				"InPlaceUpdatesSingleControlPlane": true,
			},
			want: updateFeatureGates{
				InPlaceUpdates: true,
			},
		},
		{
			name: "DistributionLifecycleはworkerから順に有効化する",
			input: map[string]bool{
				"InPlaceUpdates":                          true,
				"DistributionLifecycle":                   true,
				"DistributionLifecycleWorker":             true,
				"DistributionLifecycleMultiControlPlane":  true,
				"DistributionLifecycleSingleControlPlane": true,
			},
			want: updateFeatureGates{
				InPlaceUpdates: true,
				DistributionLifecycle: updateDistributionLifecycleFeatureGates{
					Enabled:            true,
					Worker:             true,
					MultiControlPlane:  true,
					SingleControlPlane: true,
				},
			},
		},
		{
			name: "DistributionLifecycleは前段階なしに後段階を有効化しない",
			input: map[string]bool{
				"InPlaceUpdates":                          true,
				"DistributionLifecycle":                   true,
				"DistributionLifecycleMultiControlPlane":  true,
				"DistributionLifecycleSingleControlPlane": true,
			},
			want: updateFeatureGates{
				InPlaceUpdates: true,
				DistributionLifecycle: updateDistributionLifecycleFeatureGates{
					Enabled: true,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := resolveUpdateFeatureGates(tt.input)
			if got != tt.want {
				t.Fatalf("resolveUpdateFeatureGates() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
