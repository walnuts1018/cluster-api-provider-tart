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

func TestPushOCIArtifactは可変または空のTagを拒否する(t *testing.T) {
	for _, tag := range []string{"", "latest"} {
		t.Run(tag, func(t *testing.T) {
			if err := pushOCIArtifact(t.Context(), t.TempDir(), tag, "registry.test.walnuts.dev/tart/ipxe"); err == nil {
				t.Fatalf("pushOCIArtifact(tag=%q) error = nil", tag)
			}
		})
	}
}
