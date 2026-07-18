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

package registrycredential

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReturnsEmptyCredentialWithoutConfigPath(t *testing.T) {
	credential, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if credential != nil {
		got, err := credential(t.Context(), "registry.test.walnuts.dev")
		if err != nil {
			t.Fatalf("credential() error = %v", err)
		}
		if got.Username != "" || got.Password != "" {
			t.Fatalf("credential() = %#v, want empty credential", got)
		}
	}
}

func TestLoadReadsDockerCompatibleConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
  "auths": {
    "registry.test.walnuts.dev": {
      "username": "octo",
      "password": "secret"
    }
  }
}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	credential, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got, err := credential(t.Context(), "registry.test.walnuts.dev")
	if err != nil {
		t.Fatalf("credential() error = %v", err)
	}
	if got.Username != "octo" || got.Password != "secret" {
		t.Fatalf("credential() = %#v", got)
	}
}
