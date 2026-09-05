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
	"bytes"
	"errors"
	"testing"
)

const renderBaseConfiguration = `version: v1alpha1
machine:
  type: worker
  token: token-a
`

func TestRenderEffectiveConfiguration(t *testing.T) {
	t.Parallel()

	strategicPatch := []byte(`machine:
  certSANs:
    - 192.0.2.10
`)
	jsonPatch := []byte(`[{"op":"add","path":"/machine/certSANs/-","value":"192.0.2.11"}]`)

	withoutPatches, err := RenderEffectiveConfiguration([]byte(renderBaseConfiguration))
	if err != nil {
		t.Fatalf("RenderEffectiveConfiguration() error = %v", err)
	}
	withPatches, err := RenderEffectiveConfiguration([]byte(renderBaseConfiguration), strategicPatch, jsonPatch)
	if err != nil {
		t.Fatalf("RenderEffectiveConfiguration() with patches error = %v", err)
	}
	if bytes.Equal(withoutPatches, withPatches) {
		t.Fatal("RenderEffectiveConfiguration() ignored configuration patches")
	}
	if bytes.Contains(withPatches, []byte("#")) {
		t.Fatal("RenderEffectiveConfiguration() retained YAML comments")
	}

	_, err = RenderEffectiveConfiguration([]byte(renderBaseConfiguration), []byte("machine: ["))
	if err == nil {
		t.Fatal("RenderEffectiveConfiguration() accepted malformed patch")
	}

	_, err = RenderEffectiveConfiguration([]byte(renderBaseConfiguration), nil)
	if !errors.Is(err, ErrConfigurationPatchEmpty) {
		t.Fatalf("RenderEffectiveConfiguration() empty patch error = %v, want ErrConfigurationPatchEmpty", err)
	}
}
