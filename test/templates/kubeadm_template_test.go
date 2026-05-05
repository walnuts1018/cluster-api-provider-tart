package templates

import (
	"bytes"
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
		{
			name: "talos",
			path: filepath.Join("..", "..", "config", "templates", "cluster-template-talos.yaml"),
			requiredKinds: []string{
				"Cluster",
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

func TestClusterTemplatesSetBootstrapFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want string
	}{
		{
			path: filepath.Join("..", "..", "config", "templates", "cluster-template-kubeadm-ubuntu.yaml"),
			want: "format: NoCloud",
		},
		{
			path: filepath.Join("..", "..", "config", "templates", "cluster-template-kubeadm-debian.yaml"),
			want: "format: Preseed",
		},
		{
			path: filepath.Join("..", "..", "config", "templates", "cluster-template-k3s-ubuntu.yaml"),
			want: "format: NoCloud",
		},
		{
			path: filepath.Join("..", "..", "config", "templates", "cluster-template-k3s-debian.yaml"),
			want: "format: Preseed",
		},
		{
			path: filepath.Join("..", "..", "config", "templates", "cluster-template-talos.yaml"),
			want: "format: Talos",
		},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("failed to read template %s: %v", tt.path, err)
			}
			if !bytes.Contains(data, []byte(tt.want)) {
				t.Fatalf("template %s does not contain %q", tt.path, tt.want)
			}
		})
	}
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
			break
		}
		if doc.Kind != "" {
			found[doc.Kind] = true
		}
	}
	return found
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
