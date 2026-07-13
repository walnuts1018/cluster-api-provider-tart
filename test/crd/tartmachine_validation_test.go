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
