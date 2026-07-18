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
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
)

func TestBuildOperationは同じ入力から同じIDとDigestを生成する(t *testing.T) {
	input := updateInput()

	first, err := BuildOperation(input)
	if err != nil {
		t.Fatalf("BuildOperation() first error = %v", err)
	}
	secondInput := updateInput()
	secondInput.Now = input.Now.Add(time.Minute)
	second, err := BuildOperation(secondInput)
	if err != nil {
		t.Fatalf("BuildOperation() second error = %v", err)
	}

	if first.Spec.OperationID != second.Spec.OperationID {
		t.Fatalf("OperationID = %q and %q, want equal", first.Spec.OperationID, second.Spec.OperationID)
	}
	if first.Spec.DesiredObjectsDigest != second.Spec.DesiredObjectsDigest {
		t.Fatalf("DesiredObjectsDigest = %q and %q, want equal",
			first.Spec.DesiredObjectsDigest, second.Spec.DesiredObjectsDigest)
	}
	if first.Spec.Type != infrastructurev1beta1.OperationTypeUpdate ||
		first.Spec.UpdateClass != infrastructurev1beta1.UpdateClassOSOnly ||
		first.Spec.TargetSlot != infrastructurev1beta1.OSSlotB {
		t.Fatalf("Operation spec = %#v, want OSOnly update to slot B", first.Spec)
	}
	if first.Spec.TargetArtifactGeneration == nil || *first.Spec.TargetArtifactGeneration != 2 {
		t.Fatalf("TargetArtifactGeneration = %v, want 2", first.Spec.TargetArtifactGeneration)
	}
}

func TestBuildOperationはdesired入力の変更でIDとDigestを変更する(t *testing.T) {
	input := updateInput()
	first, err := BuildOperation(input)
	if err != nil {
		t.Fatalf("BuildOperation() error = %v", err)
	}
	input.TartMachine = input.TartMachine.DeepCopy()
	input.TartMachine.Spec.Image.Ref = artifactRef("c")
	changed, err := BuildOperation(input)
	if err != nil {
		t.Fatalf("BuildOperation(changed) error = %v", err)
	}
	if first.Spec.OperationID == changed.Spec.OperationID {
		t.Fatal("OperationID did not change with desired TartMachine")
	}
	if first.Spec.DesiredObjectsDigest == changed.Spec.DesiredObjectsDigest {
		t.Fatal("DesiredObjectsDigest did not change with desired TartMachine")
	}
}

func TestBuildOperationは更新前提違反を拒否する(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*StartInput)
	}{
		{
			name: "update policy is Replace",
			mutate: func(input *StartInput) {
				input.TartMachine.Spec.UpdatePolicy.Mode = infrastructurev1beta1.UpdateModeReplace
			},
		},
		{
			name: "active slot is empty",
			mutate: func(input *StartInput) {
				input.TartMachine.Status.ActiveSlot = ""
			},
		},
		{
			name: "host is not Provisioned",
			mutate: func(input *StartInput) {
				input.Host.Status.Phase = infrastructurev1beta1.TartHostPhaseUpdating
			},
		},
		{
			name: "host reference UID differs",
			mutate: func(input *StartInput) {
				input.TartMachine.Status.HostRef.UID = types.UID("other-host")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := updateInput()
			tt.mutate(&input)
			if _, err := BuildOperation(input); err == nil {
				t.Fatal("BuildOperation() error = nil, want validation error")
			}
		})
	}
}

func updateInput() StartInput {
	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-a",
			Namespace: "default",
			UID:       types.UID("capi-machine-a"),
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: "sample",
			Version:     "v1.36.0",
			ProviderID:  "tart://host-a",
		},
	}
	tartMachine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-a",
			Namespace: "default",
			UID:       types.UID("tart-machine-a"),
		},
		Spec: infrastructurev1beta1.TartMachineSpec{
			ProviderID:      "tart://host-a",
			Image:           infrastructurev1beta1.ImageSpec{Ref: artifactRef("b")},
			PlatformProfile: "amd64-uefi-ab-ubuntu-24.04-kubeadm/v1",
			UpdatePolicy: infrastructurev1beta1.UpdatePolicy{
				Mode: infrastructurev1beta1.UpdateModeInPlace,
			},
			DeletionPolicy: infrastructurev1beta1.DeletionPolicyWipeAll,
		},
		Status: infrastructurev1beta1.TartMachineStatus{
			HostRef: &infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "host-a",
				UID:       types.UID("host-a"),
			},
			ActiveSlot:           infrastructurev1beta1.OSSlotA,
			InstalledImageDigest: testDigest("a"),
		},
	}
	host := &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "host-a",
			Namespace: "default",
			UID:       types.UID("host-a"),
		},
		Status: infrastructurev1beta1.TartHostStatus{
			Phase: infrastructurev1beta1.TartHostPhaseProvisioned,
		},
	}
	return StartInput{
		Machine:                  machine,
		TartMachine:              tartMachine,
		BootstrapConfig:          runtime.RawExtension{Raw: []byte(`{"spec":{"payload":"same"}}`)},
		Host:                     host,
		PlanDigest:               testDigest("d"),
		TargetImageDigest:        testDigest("b"),
		TargetArtifactGeneration: 2,
		Now:                      time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC),
	}
}

func artifactRef(fill string) string {
	return "oci://registry.test.walnuts.dev/os/ubuntu@sha256:" + repeat(fill, 64)
}

func testDigest(fill string) string {
	return "sha256:" + repeat(fill, 64)
}

func repeat(value string, count int) string {
	result := ""
	for range count {
		result += value
	}
	return result
}
