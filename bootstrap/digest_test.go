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

package bootstrap

import (
	"errors"
	"testing"
)

func TestDigestEffectiveConfiguration(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		first      string
		second     string
		sameDigest bool
		wantErr    error
	}{
		"normalizes YAML representation": {
			first:      "version: v1alpha1\nmachine:\n  type: worker\n  token: token-a\n",
			second:     "# equivalent input\nmachine:\n  token: token-a\n  type: worker\nversion: v1alpha1\n",
			sameDigest: true,
		},
		"redacts machine token": {
			first:      "version: v1alpha1\nmachine:\n  type: worker\n  token: token-a\n",
			second:     "version: v1alpha1\nmachine:\n  type: worker\n  token: token-b\n",
			sameDigest: true,
		},
		"retains non-secret changes": {
			first:  "version: v1alpha1\nmachine:\n  type: worker\n  token: token-a\n",
			second: "version: v1alpha1\nmachine:\n  type: controlplane\n  token: token-a\n",
		},
		"rejects empty configuration": {
			wantErr: ErrCompleteConfigurationEmpty,
		},
		"rejects malformed configuration": {
			first:   "version: [",
			wantErr: ErrEffectiveConfigurationInvalid,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			first, err := DigestEffectiveConfiguration([]byte(tt.first))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("DigestEffectiveConfiguration() error = %v, want %v", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("DigestEffectiveConfiguration(first) error = %v", err)
			}

			second, err := DigestEffectiveConfiguration([]byte(tt.second))
			if err != nil {
				t.Fatalf("DigestEffectiveConfiguration(second) error = %v", err)
			}
			if (first == second) != tt.sameDigest {
				t.Fatalf("digest equality = %t, want %t", first == second, tt.sameDigest)
			}
		})
	}
}
