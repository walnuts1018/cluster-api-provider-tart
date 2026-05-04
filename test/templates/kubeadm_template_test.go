package templates

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/util/yaml"
)

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
