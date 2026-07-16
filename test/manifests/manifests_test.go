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
	"slices"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
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

func TestManagerRoleAllowsTartMachineMetadataPatch(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "rbac", "role.yaml"))
	if err != nil {
		t.Fatalf("failed to read manager role: %v", err)
	}

	var role rbacv1.ClusterRole
	if err := yaml.Unmarshal(data, &role); err != nil {
		t.Fatalf("failed to parse manager role: %v", err)
	}

	rule := findPolicyRule(role.Rules, "infrastructure.cluster.x-k8s.io", "tartmachines")
	if rule == nil {
		t.Fatal("manager role missing tartmachines rule")
	}
	for _, verb := range []string{"patch", "update"} {
		if !contains(rule.Verbs, verb) {
			t.Fatalf("tartmachines rule missing %q verb: %v", verb, rule.Verbs)
		}
	}
}

func TestProvisioningE2EOverlayConfiguresInitialProvisioning(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "provisioning-e2e", "kustomization.yaml"))
	if err != nil {
		t.Fatalf("failed to read provisioning e2e kustomization: %v", err)
	}

	text := string(data)
	for _, want := range []string{
		"e2e-provisioning-keys",
		"os-artifact-public.pem=generated/os-artifact-public.pem",
		"agent-plan-private.pem=generated/agent-plan-private.pem",
		"--os-artifact-key-id=e2e-os-artifact",
		"--os-artifact-public-key-file=/etc/tart-e2e/os-artifact-public.pem",
		"--agent-plan-key-id=e2e-agent-plan",
		"--agent-plan-private-key-file=/etc/tart-e2e/agent-plan-private.pem",
		"mountPath: /etc/tart-e2e",
		"path: /spec/template/spec/securityContext/fsGroup",
		"value: 65532",
		"defaultMode: 0440",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("provisioning e2e overlay missing %q", want)
		}
	}
}

func TestProvisioningE2EClusterctlConfigAcceptsGeneratedArtifactRef(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "e2e", "config", "tart.yaml"))
	if err != nil {
		t.Fatalf("failed to read provisioning e2e clusterctl config: %v", err)
	}

	text := string(data)
	for _, want := range []string{
		`OS_ARTIFACT_REF: "${OS_ARTIFACT_REF}"`,
		`OS_ARTIFACT_REGISTRY: "${OS_ARTIFACT_REGISTRY}"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("provisioning e2e clusterctl config missing %q", want)
		}
	}
}

func findPolicyRule(rules []rbacv1.PolicyRule, apiGroup, resource string) *rbacv1.PolicyRule {
	for i := range rules {
		rule := &rules[i]
		if contains(rule.APIGroups, apiGroup) && contains(rule.Resources, resource) {
			return rule
		}
	}
	return nil
}

func contains(values []string, want string) bool {
	return slices.Contains(values, want)
}
