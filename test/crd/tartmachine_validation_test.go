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

package crd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

func TestTartMachineCRDAllowsRealisticBootParameters(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "crd", "bases", "infrastructure.cluster.x-k8s.io_tartmachines.yaml"))
	if err != nil {
		t.Fatalf("failed to read TartMachine CRD: %v", err)
	}

	text := string(data)
	for _, rejectedPattern := range []string{
		"^[a-zA-Z0-9._-]+=[a-zA-Z0-9._-]+$",
		"^https?://[a-zA-Z0-9._-]+(?::\\d+)?(/[a-zA-Z0-9._-]+)*$|^/([a-zA-Z0-9._-]+)+$",
	} {
		if strings.Contains(text, rejectedPattern) {
			t.Fatalf("CRD still contains restrictive validation pattern %q", rejectedPattern)
		}
	}
}

func TestTartMachineCRDRequiresArtifactModel(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "crd", "bases", "infrastructure.cluster.x-k8s.io_tartmachines.yaml"))
	if err != nil {
		t.Fatalf("failed to read TartMachine CRD: %v", err)
	}

	text := string(data)
	for _, want := range []string{
		"ref:\n                    description: ref is a digest-pinned OCI artifact reference.",
		"platformProfile:\n                description: platformProfile identifies the versioned platform configuration",
		"mode: Replace",
		"- deletionPolicy",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("CRD missing %q", want)
		}
	}
}

func TestTartMachineTemplateCRDRequiresArtifactModel(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "crd", "bases", "infrastructure.cluster.x-k8s.io_tartmachinetemplates.yaml"))
	if err != nil {
		t.Fatalf("failed to read TartMachineTemplate CRD: %v", err)
	}

	text := string(data)
	for _, want := range []string{
		"ref:\n                            description: ref is a digest-pinned OCI artifact reference.",
		"platformProfile:\n                        description: platformProfile identifies the versioned platform",
		"mode: Replace",
		"- deletionPolicy",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("CRD missing %q", want)
		}
	}
}

func TestCAPIContractCRDLabels(t *testing.T) {
	for _, name := range []string{
		"infrastructure.cluster.x-k8s.io_tartclusters.yaml",
		"infrastructure.cluster.x-k8s.io_tartclustertemplates.yaml",
		"infrastructure.cluster.x-k8s.io_tartmachines.yaml",
		"infrastructure.cluster.x-k8s.io_tartmachinetemplates.yaml",
	} {
		t.Run(name, func(t *testing.T) {
			crd := readCRD(t, name)

			for label, want := range map[string]string{
				"cluster.x-k8s.io/v1beta1": "v1beta1",
				"cluster.x-k8s.io/v1beta2": "v1beta1",
			} {
				if got := crd.Labels[label]; got != want {
					t.Fatalf("CRD %s label %s = %q, want %q", crd.Name, label, got, want)
				}
			}
		})
	}
}

func readCRD(t *testing.T, name string) *apiextensionsv1.CustomResourceDefinition {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", "config", "crd", "bases", name))
	if err != nil {
		t.Fatalf("failed to read CRD %s: %v", name, err)
	}

	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := yaml.Unmarshal(data, crd); err != nil {
		t.Fatalf("failed to parse CRD %s: %v", name, err)
	}
	return crd
}
