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

package ociremote

import (
	"testing"

	"oras.land/oras-go/v2/registry/remote"
)

func TestAllowLoopbackPlainHTTP(t *testing.T) {
	tests := []struct {
		name      string
		repo      string
		wantPlain bool
	}{
		{name: "localhost with port", repo: "localhost:5000/tart/ubuntu", wantPlain: true},
		{name: "ipv4 loopback", repo: "127.0.0.1:5000/tart/ubuntu", wantPlain: true},
		{name: "ipv6 loopback", repo: "[::1]:5000/tart/ubuntu", wantPlain: true},
		{name: "external registry", repo: "registry.sample.walnuts.dev/tart/ubuntu", wantPlain: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository, err := remote.NewRepository(tt.repo)
			if err != nil {
				t.Fatalf("failed to create repository: %v", err)
			}

			AllowLoopbackPlainHTTP(repository)
			if repository.PlainHTTP != tt.wantPlain {
				t.Fatalf("PlainHTTP = %v, want %v", repository.PlainHTTP, tt.wantPlain)
			}
		})
	}
}
