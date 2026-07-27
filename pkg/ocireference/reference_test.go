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

package ocireference

import (
	"strings"
	"testing"
)

func TestParseはOCI画像参照の全形式を受け入れる(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + strings.Repeat("a", 64)
	for _, reference := range []string{
		"oci://ghcr.io/walnuts1018/cluster-api-provider-tart-os-ubuntu-26.04-amd64-kubeadm",
		"oci://ghcr.io/walnuts1018/cluster-api-provider-tart-os-ubuntu-26.04-amd64-kubeadm:v0.1.12",
		"oci://ghcr.io/walnuts1018/cluster-api-provider-tart-os-ubuntu-26.04-amd64-kubeadm@" + digest,
		"oci://ghcr.io/walnuts1018/cluster-api-provider-tart-os-ubuntu-26.04-amd64-kubeadm:v0.1.12@" + digest,
	} {
		if _, err := Parse(reference); err != nil {
			t.Errorf("Parse(%q) error = %v", reference, err)
		}
	}
}

func TestParseは不正なOCI画像参照を拒否する(t *testing.T) {
	t.Parallel()

	for _, reference := range []string{
		"https://ghcr.io/walnuts1018/os:v0.1.12",
		"oci://ghcr.io/invalid repository:v0.1.12",
		"oci://ghcr.io/walnuts1018/os:invalid tag",
	} {
		if _, err := Parse(reference); err == nil {
			t.Errorf("Parse(%q) error = nil", reference)
		}
	}
}
