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

package templates

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestClusterTemplatesContainRequiredKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		path          string
		requiredKinds []string
	}{
		{
			name: "kubeadm",
			path: filepath.Join("..", "..", "config", "templates", "cluster-template-kubeadm.yaml"),
			requiredKinds: []string{
				"Cluster",
				"KubeadmControlPlane",
				"KubeadmConfigTemplate",
				"MachineDeployment",
				"TartCluster",
				"TartMachineTemplate",
			},
		},
		{
			name: "kubeadm ubuntu",
			path: filepath.Join("..", "..", "config", "templates", "cluster-template-kubeadm-ubuntu.yaml"),
			requiredKinds: []string{
				"Cluster",
				"KubeadmControlPlane",
				"KubeadmConfigTemplate",
				"MachineDeployment",
				"TartCluster",
				"TartMachineTemplate",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			found := readTemplateKinds(t, tt.path)
			for _, kind := range tt.requiredKinds {
				if !found[kind] {
					t.Fatalf("template %s does not contain %s", tt.path, kind)
				}
			}
		})
	}
}

func TestClusterTemplatesUseV1Beta1ArtifactModel(t *testing.T) {
	t.Parallel()

	assertFilesContain(t, "template", []fileExpectation{
		{
			path: filepath.Join("..", "..", "config", "templates", "cluster-template-kubeadm.yaml"),
			want: "apiVersion: infrastructure.cluster.x-k8s.io/v1beta1",
		},
		{
			path: filepath.Join("..", "..", "config", "templates", "cluster-template-kubeadm-ubuntu.yaml"),
			want: "platformProfile: ${PLATFORM_PROFILE:=amd64-uefi-ab-ubuntu-24.04-kubeadm/v1}",
		},
		{
			path: filepath.Join("..", "..", "config", "templates", "cluster-template-kubeadm.yaml"),
			want: "ref: ${OS_ARTIFACT_REF:=oci://registry.sample.walnuts.dev/tart/ubuntu@sha256:",
		},
	})
}

func TestSamplesUseV1Beta1ArtifactModel(t *testing.T) {
	t.Parallel()

	assertFilesContain(t, "sample", []fileExpectation{
		{
			path: filepath.Join("..", "..", "config", "samples", "cluster-kubeadm-ubuntu.yaml"),
			want: "apiVersion: infrastructure.cluster.x-k8s.io/v1beta1",
		},
		{
			path: filepath.Join("..", "..", "config", "samples", "cluster-kubeadm-ubuntu.yaml"),
			want: "platformProfile: amd64-uefi-ab-ubuntu-24.04-kubeadm/v1",
		},
	})
}

func readTemplateKinds(t *testing.T, path string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read template %s: %v", path, err)
	}
	dec := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	found := map[string]bool{}
	for {
		var doc struct {
			Kind string `json:"kind"`
		}
		err := dec.Decode(&doc)
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("failed to decode template %s: %v", path, err)
		}
		if doc.Kind != "" {
			found[doc.Kind] = true
		}
	}
	return found
}

type fileExpectation struct {
	path string
	want string
}

func assertFilesContain(t *testing.T, fileKind string, tests []fileExpectation) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("failed to read %s %s: %v", fileKind, tt.path, err)
			}
			if !bytes.Contains(data, []byte(tt.want)) {
				t.Fatalf("%s %s does not contain %q", fileKind, tt.path, tt.want)
			}
		})
	}
}

func TestKubeadmClusterTemplateContainsRequiredKinds(t *testing.T) {
	path := filepath.Join("..", "..", "config", "templates", "cluster-template-kubeadm.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read kubeadm cluster template: %v", err)
	}

	dec := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	found := map[string]bool{}
	for {
		var doc struct {
			Kind string `json:"kind"`
		}
		err := dec.Decode(&doc)
		if err != nil {
			break
		}
		if doc.Kind != "" {
			found[doc.Kind] = true
		}
	}

	requiredKinds := []string{
		"Cluster",
		"KubeadmControlPlane",
		"KubeadmConfigTemplate",
		"MachineDeployment",
		"TartCluster",
		"TartMachineTemplate",
	}
	for _, kind := range requiredKinds {
		if !found[kind] {
			t.Fatalf("template does not contain %s", kind)
		}
	}
}
