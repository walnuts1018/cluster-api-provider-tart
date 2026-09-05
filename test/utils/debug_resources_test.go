package utils

import (
	"strings"
	"testing"
)

func TestDebugClusterAPIResourcesAvoidsRemovedResourceNames(t *testing.T) {
	removed := []string{
		"clustersets",
		"machineclasses",
		"machinehealthchecksets",
		"kubeadmconfigconfigs",
		"ipaddresspools",
	}

	for _, resource := range debugClusterAPIResources() {
		if resource.Filename == "" {
			t.Fatal("Filename must not be empty")
		}
		if !strings.Contains(resource.Resource, ".") {
			t.Fatalf("Resource %q must use a fully qualified resource name", resource.Resource)
		}
		for _, removedName := range removed {
			if strings.Contains(resource.Resource, removedName) {
				t.Fatalf("Resource %q contains removed resource name %q", resource.Resource, removedName)
			}
		}
	}
}

func TestDebugClusterAPIResourcesIncludesTartOperationState(t *testing.T) {
	want := map[string]bool{
		"tartclusters.infrastructure.cluster.x-k8s.io":       false,
		"tarthosts.infrastructure.cluster.x-k8s.io":          false,
		"tarthostoperations.infrastructure.cluster.x-k8s.io": false,
		"tartmachines.infrastructure.cluster.x-k8s.io":       false,
	}

	for _, resource := range debugClusterAPIResources() {
		if _, ok := want[resource.Resource]; ok {
			want[resource.Resource] = true
		}
	}
	for resource, found := range want {
		if !found {
			t.Fatalf("Resource list missing %q", resource)
		}
	}
}
