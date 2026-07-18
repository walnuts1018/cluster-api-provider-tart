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
				changes.DesiredTartMachine.ImageRef = artifactRef("b")
			},
			allowed:  true,
			wantPath: FieldTartMachineImageRef,
		},
		{
			name: "update policy",
			mutate: func(changes *ChangeSet) {
				changes.DesiredTartMachine.UpdatePolicy = "InPlace"
			},
			allowed:  true,
			wantPath: FieldTartMachineUpdatePolicy,
		},
		{
			name: "Kubernetes version",
			mutate: func(changes *ChangeSet) {
				changes.DesiredMachine.Version = "v1.35.0"
			},
			wantPath: FieldMachineVersion,
		},
		{
			name: "bootstrap payload",
			mutate: func(changes *ChangeSet) {
				changes.DesiredBootstrapConfig = map[string]string{"payload": "changed"}
			},
			wantPath: FieldBootstrapConfig,
		},
		{
			name: "platform profile",
			mutate: func(changes *ChangeSet) {
				changes.DesiredTartMachine.PlatformProfile = "amd64-uefi-ab/v2"
			},
			wantPath: FieldTartMachinePlatformProfile,
		},
		{
			name: "host selector",
			mutate: func(changes *ChangeSet) {
				changes.DesiredTartMachine.HostSelector = map[string]string{"rack": "rack-b"}
			},
			wantPath: FieldTartMachineHostSelector,
		},
		{
			name: "provider ID",
			mutate: func(changes *ChangeSet) {
				changes.DesiredTartMachine.ProviderID = "tart://different-host"
			},
			wantPath: FieldTartMachineProviderID,
		},
		{
			name: "deletion policy",
			mutate: func(changes *ChangeSet) {
				changes.DesiredTartMachine.DeletionPolicy = "RetainData"
			},
			wantPath: FieldTartMachineDeletionPolicy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes := baseChangeSet()
			tt.mutate(&changes)

			got := Classify(changes)
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
	got := Classify(baseChangeSet())
	if got.CanUpdateInPlace() {
		t.Fatal("CanUpdateInPlace() = true, want false")
	}
	if len(got.Changed) != 0 {
		t.Fatalf("Changed = %v, want empty", got.Changed)
	}
}

func TestClassifyは許可差分と拒否差分の混在を拒否する(t *testing.T) {
	changes := baseChangeSet()
	changes.DesiredTartMachine.ImageRef = artifactRef("b")
	changes.DesiredMachine.Version = "v1.35.0"

	got := Classify(changes)
	if got.CanUpdateInPlace() {
		t.Fatal("CanUpdateInPlace() = true, want false")
	}
	if !slices.Contains(got.Allowed, FieldTartMachineImageRef) {
		t.Errorf("Allowed = %v, want image ref", got.Allowed)
	}
	if !slices.Contains(got.Rejected, FieldMachineVersion) {
		t.Errorf("Rejected = %v, want machine version", got.Rejected)
	}
	if slices.Contains(got.Rejected, FieldMachineSpec) {
		t.Errorf("Rejected = %v, must not contain machine spec when only version changed", got.Rejected)
	}
}

func baseChangeSet() ChangeSet {
	return ChangeSet{
		CurrentMachine: MachineSpecSnapshot{
			Version: "v1.34.0",
			Spec:    map[string]string{"clusterName": "sample", "providerID": "tart://host-1"},
		},
		DesiredMachine: MachineSpecSnapshot{
			Version: "v1.34.0",
			Spec:    map[string]string{"clusterName": "sample", "providerID": "tart://host-1"},
		},
		CurrentTartMachine: TartMachineSpecSnapshot{
			ImageRef:        artifactRef("a"),
			UpdatePolicy:    "Replace",
			PlatformProfile: "amd64-uefi-ab/v1",
			HostSelector:    map[string]string{"rack": "rack-a"},
			ProviderID:      "tart://host-1",
			DeletionPolicy:  "WipeAll",
		},
		DesiredTartMachine: TartMachineSpecSnapshot{
			ImageRef:        artifactRef("a"),
			UpdatePolicy:    "Replace",
			PlatformProfile: "amd64-uefi-ab/v1",
			HostSelector:    map[string]string{"rack": "rack-a"},
			ProviderID:      "tart://host-1",
			DeletionPolicy:  "WipeAll",
		},
		CurrentBootstrapConfig: map[string]string{"payload": "same"},
		DesiredBootstrapConfig: map[string]string{"payload": "same"},
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
