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

package inplaceupdate

import (
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

func TestClassifyはOSOnly差分だけを許可する(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*ChangeSet)
		allowed  bool
		wantPath FieldPath
	}{
		{
			name: "image ref",
			mutate: func(changes *ChangeSet) {
				changes.DesiredTartMachine.Spec.Image.Ref = artifactRef("b")
			},
			allowed:  true,
			wantPath: FieldTartMachineImageRef,
		},
		{
			name: "update policy",
			mutate: func(changes *ChangeSet) {
				changes.DesiredTartMachine.Spec.UpdatePolicy.Mode = infrastructurev1beta1.UpdateModeInPlace
			},
			allowed:  true,
			wantPath: FieldTartMachineUpdatePolicy,
		},
		{
			name: "Kubernetes version",
			mutate: func(changes *ChangeSet) {
				changes.DesiredMachine.Spec.Version = "v1.35.0"
			},
			wantPath: FieldMachineVersion,
		},
		{
			name: "bootstrap payload",
			mutate: func(changes *ChangeSet) {
				changes.DesiredBootstrapConfig.Raw = []byte(`{"apiVersion":"bootstrap.cluster.x-k8s.io/v1beta2","kind":"KubeadmConfig","spec":{"payload":"changed"}}`)
			},
			wantPath: FieldBootstrapConfig,
		},
		{
			name: "platform profile",
			mutate: func(changes *ChangeSet) {
				changes.DesiredTartMachine.Spec.PlatformProfile = "amd64-uefi-ab/v2"
			},
			wantPath: FieldTartMachinePlatformProfile,
		},
		{
			name: "host selector",
			mutate: func(changes *ChangeSet) {
				changes.DesiredTartMachine.Spec.HostSelector.MatchLabels["rack"] = "rack-b"
			},
			wantPath: FieldTartMachineHostSelector,
		},
		{
			name: "provider ID",
			mutate: func(changes *ChangeSet) {
				changes.DesiredTartMachine.Spec.ProviderID = "tart://different-host"
			},
			wantPath: FieldTartMachineProviderID,
		},
		{
			name: "deletion policy",
			mutate: func(changes *ChangeSet) {
				changes.DesiredTartMachine.Spec.DeletionPolicy = infrastructurev1beta1.DeletionPolicyRetainData
			},
			wantPath: FieldTartMachineDeletionPolicy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes := baseChangeSet()
			tt.mutate(&changes)

			got, err := Classify(changes)
			if err != nil {
				t.Fatalf("Classify() error = %v", err)
			}
			if got.CanUpdateInPlace() != tt.allowed {
				t.Fatalf("CanUpdateInPlace() = %t, want %t; changed=%v rejected=%v",
					got.CanUpdateInPlace(), tt.allowed, got.Changed, got.Rejected)
			}
			if !slices.Contains(got.Changed, tt.wantPath) {
				t.Errorf("Changed = %v, want path %q", got.Changed, tt.wantPath)
			}
			if tt.allowed && !slices.Contains(got.Allowed, tt.wantPath) {
				t.Errorf("Allowed = %v, want path %q", got.Allowed, tt.wantPath)
			}
			if !tt.allowed && !slices.Contains(got.Rejected, tt.wantPath) {
				t.Errorf("Rejected = %v, want path %q", got.Rejected, tt.wantPath)
			}
		})
	}
}

func TestClassifyは差分なしを更新対象にしない(t *testing.T) {
	got, err := Classify(baseChangeSet())
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if got.CanUpdateInPlace() {
		t.Fatal("CanUpdateInPlace() = true, want false")
	}
	if len(got.Changed) != 0 {
		t.Fatalf("Changed = %v, want empty", got.Changed)
	}
}

func TestClassifyは許可差分と拒否差分の混在を拒否する(t *testing.T) {
	changes := baseChangeSet()
	changes.DesiredTartMachine.Spec.Image.Ref = artifactRef("b")
	changes.DesiredMachine.Spec.Version = "v1.35.0"

	got, err := Classify(changes)
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if got.CanUpdateInPlace() {
		t.Fatal("CanUpdateInPlace() = true, want false")
	}
	if !slices.Contains(got.Allowed, FieldTartMachineImageRef) {
		t.Errorf("Allowed = %v, want image ref", got.Allowed)
	}
	if !slices.Contains(got.Rejected, FieldMachineVersion) {
		t.Errorf("Rejected = %v, want machine version", got.Rejected)
	}
}

func TestClassifyはBootstrapのmetadata差分を無視する(t *testing.T) {
	changes := baseChangeSet()
	changes.DesiredBootstrapConfig.Raw = []byte(`{"apiVersion":"bootstrap.cluster.x-k8s.io/v1beta2","kind":"KubeadmConfig","metadata":{"resourceVersion":"2"},"spec":{"payload":"same"}}`)

	got, err := Classify(changes)
	if err != nil {
		t.Fatalf("Classify() error = %v", err)
	}
	if len(got.Changed) != 0 {
		t.Fatalf("Changed = %v, want empty", got.Changed)
	}
}

func TestClassifyは不正なBootstrapConfigを拒否する(t *testing.T) {
	changes := baseChangeSet()
	changes.DesiredBootstrapConfig.Raw = []byte(`{`)

	if _, err := Classify(changes); err == nil {
		t.Fatal("Classify() error = nil, want malformed BootstrapConfig error")
	}
}

func baseChangeSet() ChangeSet {
	currentMachine := clusterv1.Machine{
		Spec: clusterv1.MachineSpec{
			ClusterName: "sample",
			Version:     "v1.34.0",
			ProviderID:  "tart://host-1",
		},
	}
	currentTartMachine := infrastructurev1beta1.TartMachine{
		Spec: infrastructurev1beta1.TartMachineSpec{
			ProviderID:      "tart://host-1",
			Image:           infrastructurev1beta1.ImageSpec{Ref: artifactRef("a")},
			PlatformProfile: "amd64-uefi-ab/v1",
			HostSelector: infrastructurev1beta1.HostSelector{
				MatchLabels: map[string]string{"rack": "rack-a"},
			},
			UpdatePolicy:   infrastructurev1beta1.UpdatePolicy{Mode: infrastructurev1beta1.UpdateModeReplace},
			DeletionPolicy: infrastructurev1beta1.DeletionPolicyWipeAll,
		},
	}
	bootstrap := runtime.RawExtension{
		Raw: []byte(`{"apiVersion":"bootstrap.cluster.x-k8s.io/v1beta2","kind":"KubeadmConfig","metadata":{"resourceVersion":"1"},"spec":{"payload":"same"}}`),
	}

	return ChangeSet{
		CurrentMachine:         currentMachine,
		DesiredMachine:         *currentMachine.DeepCopy(),
		CurrentTartMachine:     currentTartMachine,
		DesiredTartMachine:     *currentTartMachine.DeepCopy(),
		CurrentBootstrapConfig: bootstrap,
		DesiredBootstrapConfig: runtime.RawExtension{Raw: slices.Clone(bootstrap.Raw)},
	}
}

func artifactRef(fill string) string {
	return "oci://registry.test.walnuts.dev/os/ubuntu@sha256:" + repeat(fill, 64)
}

func repeat(value string, count int) string {
	var result strings.Builder
	for range count {
		result.WriteString(value)
	}
	return result.String()
}
