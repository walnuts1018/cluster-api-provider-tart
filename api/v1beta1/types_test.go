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

package v1beta1

import (
	"encoding/json"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
)

func TestAddToSchemeRegistersStorageTypes(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}

	objects := []runtime.Object{
		&TartCluster{},
		&TartClusterTemplate{},
		&TartHost{},
		&TartHostOperation{},
		&TartMachine{},
		&TartMachineTemplate{},
	}
	for _, object := range objects {
		gvks, _, err := scheme.ObjectKinds(object)
		if err != nil {
			t.Fatalf("ObjectKinds(%T) error = %v", object, err)
		}
		if len(gvks) != 1 {
			t.Fatalf("ObjectKinds(%T) returned %d GVKs, want 1", object, len(gvks))
		}
		if gvks[0].Version != "v1beta1" {
			t.Fatalf("ObjectKinds(%T) version = %q, want v1beta1", object, gvks[0].Version)
		}
	}
}

func TestTartMachineTemplateCannotCarryProviderID(t *testing.T) {
	t.Parallel()

	template := TartMachineTemplate{
		Spec: TartMachineTemplateSpec{
			Template: TartMachineTemplateResource{
				Spec: TartMachineTemplateResourceSpec{
					Image:           ImageSpec{Ref: "oci://registry.sample.walnuts.dev/tart/ubuntu@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
					PlatformProfile: "amd64-uefi-ab/v1",
					DeletionPolicy:  DeletionPolicyWipeAll,
				},
			},
		},
	}

	data, err := json.Marshal(template)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(data), "providerID") {
		t.Fatalf("serialized template contains controller-managed providerID: %s", data)
	}
}

func TestTartHostOperationPreservesDesiredObjectsDigest(t *testing.T) {
	t.Parallel()

	const digest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	operation := TartHostOperation{
		Spec: TartHostOperationSpec{
			DesiredObjectsDigest: digest,
		},
	}

	data, err := json.Marshal(operation)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var decoded TartHostOperation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if decoded.Spec.DesiredObjectsDigest != digest {
		t.Fatalf("desiredObjectsDigest = %q, want %q", decoded.Spec.DesiredObjectsDigest, digest)
	}
}
