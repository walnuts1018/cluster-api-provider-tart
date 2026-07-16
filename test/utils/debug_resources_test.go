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
