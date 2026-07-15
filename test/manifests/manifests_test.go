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

package manifests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerManifestEnablesEmbeddedBootstrapServers(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "manager", "manager.yaml"))
	if err != nil {
		t.Fatalf("failed to read manager manifest: %v", err)
	}

	text := string(data)
	for _, want := range []string{
		"hostNetwork: true",
		"dnsPolicy: Default",
		"containerPort: 67",
		"containerPort: 69",
		"protocol: UDP",
		"--bootstrap-advertise-address=$(POD_IP)",
		"- NET_BIND_SERVICE",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("manager manifest missing %q", want)
		}
	}
}

func TestExternalBootstrapOverlayDisablesEmbeddedBootstrapServers(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "external-bootstrap", "kustomization.yaml"))
	if err != nil {
		t.Fatalf("failed to read external bootstrap kustomization: %v", err)
	}

	text := string(data)
	for _, want := range []string{
		"../default",
		"--bootstrap-bind-address=0",
		"--tftp-bind-address=0",
		"--ipxe-bind-address=0",
		"/spec/template/spec/hostNetwork",
		"/spec/template/spec/dnsPolicy",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("external bootstrap overlay missing %q", want)
		}
	}
}

func TestWebhookPatchReusesRuntimeExtensionPort(t *testing.T) {
	managerData, err := os.ReadFile(filepath.Join("..", "..", "config", "manager", "manager.yaml"))
	if err != nil {
		t.Fatalf("failed to read manager manifest: %v", err)
	}
	patchData, err := os.ReadFile(filepath.Join("..", "..", "config", "default", "manager_webhook_patch.yaml"))
	if err != nil {
		t.Fatalf("failed to read webhook patch: %v", err)
	}

	if got := strings.Count(string(managerData), "containerPort: 9443"); got != 1 {
		t.Fatalf("manager manifest has %d port 9443 definitions, want 1", got)
	}
	if strings.Contains(string(patchData), "containerPort: 9443") {
		t.Fatal("webhook patch must reuse the existing manager port 9443")
	}
}
