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
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/operation"
)

func TestValidateRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		registry string
		wantErr  bool
	}{
		{name: "hostname", registry: "registry.sample.walnuts.dev"},
		{name: "hostname and port", registry: "registry.sample.walnuts.dev:5443"},
		{name: "empty", registry: "", wantErr: true},
		{name: "wildcard", registry: "*.sample.walnuts.dev", wantErr: true},
		{name: "path", registry: "registry.sample.walnuts.dev/team", wantErr: true},
		{name: "scheme", registry: "https://registry.sample.walnuts.dev", wantErr: true},
		{name: "invalid port", registry: "registry.sample.walnuts.dev:70000", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateRegistry(tt.registry)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateRegistry(%q) error = %v, wantErr %t", tt.registry, err, tt.wantErr)
			}
		})
	}
}

func TestTartClusterValidator(t *testing.T) {
	t.Parallel()

	validator := TartClusterCustomValidator{}
	valid := &infrastructurev1beta1.TartCluster{
		Spec: infrastructurev1beta1.TartClusterSpec{
			ArtifactPolicy: infrastructurev1beta1.ArtifactPolicy{
				AllowedRegistries: []string{"registry.sample.walnuts.dev"},
			},
		},
	}
	if _, err := validator.ValidateCreate(t.Context(), valid); err != nil {
		t.Fatalf("ValidateCreate() error = %v", err)
	}

	for _, registries := range [][]string{nil, {}, {"*.sample.walnuts.dev"}, {"registry.sample.walnuts.dev/team"}} {
		invalid := valid.DeepCopy()
		invalid.Spec.ArtifactPolicy.AllowedRegistries = registries
		if _, err := validator.ValidateCreate(t.Context(), invalid); err == nil {
			t.Fatalf("ValidateCreate() registries = %v, want error", registries)
		}
	}
}

func TestTartHostValidatorRejectsUnstableRootDeviceHint(t *testing.T) {
	t.Parallel()

	validator := TartHostCustomValidator{}
	host := &infrastructurev1beta1.TartHost{
		Spec: infrastructurev1beta1.TartHostSpec{
			RootDeviceHints: infrastructurev1beta1.RootDeviceHints{
				DeviceName:   "/dev/sda",
				MinSizeBytes: 1,
			},
		},
	}
	if _, err := validator.ValidateCreate(t.Context(), host); err == nil {
		t.Fatal("ValidateCreate() error = nil, want unstable device path rejection")
	}

	host.Spec.RootDeviceHints.DeviceName = "/dev/disk/by-id/wwn-0x5000000000000001"
	if _, err := validator.ValidateCreate(t.Context(), host); err != nil {
		t.Fatalf("ValidateCreate() error = %v", err)
	}
}

func TestMachineDefaultingAndValidation(t *testing.T) {
	t.Parallel()

	machine := &infrastructurev1beta1.TartMachine{
		Spec: infrastructurev1beta1.TartMachineSpec{
			Image: infrastructurev1beta1.ImageSpec{
				Ref: "oci://registry.sample.walnuts.dev/tart/ubuntu@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			},
		},
	}
	if err := (&TartMachineCustomDefaulter{}).Default(t.Context(), machine); err != nil {
		t.Fatalf("Default() error = %v", err)
	}
	if machine.Spec.UpdatePolicy.Mode != infrastructurev1beta1.UpdateModeReplace {
		t.Fatalf("update mode = %q, want %q", machine.Spec.UpdatePolicy.Mode, infrastructurev1beta1.UpdateModeReplace)
	}
	if _, err := (&TartMachineCustomValidator{}).ValidateCreate(t.Context(), machine); err != nil {
		t.Fatalf("ValidateCreate() error = %v", err)
	}

	machine.Spec.Image.Ref = "oci://registry.sample.walnuts.dev/tart/ubuntu:latest"
	if _, err := (&TartMachineCustomValidator{}).ValidateCreate(t.Context(), machine); err == nil {
		t.Fatal("ValidateCreate() error = nil, want mutable tag rejection")
	}
}

func TestTartMachineProvisionedIsMonotonic(t *testing.T) {
	t.Parallel()

	provisioned := true
	notProvisioned := false
	oldMachine := validMachine()
	oldMachine.Status.Initialization.Provisioned = &provisioned
	newMachine := oldMachine.DeepCopy()
	newMachine.Status.Initialization.Provisioned = &notProvisioned

	if _, err := (&TartMachineCustomValidator{}).ValidateUpdate(t.Context(), oldMachine, newMachine); err == nil {
		t.Fatal("ValidateUpdate() error = nil, want monotonic provisioned rejection")
	}
}

func TestTartHostOperationValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*infrastructurev1beta1.TartHostOperation)
	}{
		{name: "valid provision"},
		{name: "deadline is required", mutate: func(operation *infrastructurev1beta1.TartHostOperation) {
			operation.Spec.Deadline = metav1.Time{}
		}},
		{name: "desired objects digest is required", mutate: func(operation *infrastructurev1beta1.TartHostOperation) {
			operation.Spec.DesiredObjectsDigest = ""
		}},
		{name: "machine UID is required", mutate: func(operation *infrastructurev1beta1.TartHostOperation) {
			operation.Spec.MachineRef.UID = ""
		}},
		{name: "update targets are required", mutate: func(operation *infrastructurev1beta1.TartHostOperation) {
			operation.Spec.Type = infrastructurev1beta1.OperationTypeUpdate
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			operation := validOperation()
			if tt.mutate != nil {
				tt.mutate(operation)
			}
			_, err := (&TartHostOperationCustomValidator{}).ValidateCreate(t.Context(), operation)
			if tt.mutate == nil && err != nil {
				t.Fatalf("ValidateCreate() error = %v", err)
			}
			if tt.mutate != nil && err == nil {
				t.Fatal("ValidateCreate() error = nil, want error")
			}
		})
	}
}

func TestTartHostOperationValidationは手動WipeAllでMachineRefを省略できる(t *testing.T) {
	t.Parallel()

	operation := validOperation()
	operation.Spec.Type = infrastructurev1beta1.OperationTypeWipeAll
	operation.Spec.MachineRef = nil

	if _, err := (&TartHostOperationCustomValidator{}).ValidateCreate(t.Context(), operation); err != nil {
		t.Fatalf("ValidateCreate() error = %v", err)
	}
}

func TestTartHostOperationRequiresDeterministicActiveName(t *testing.T) {
	t.Parallel()

	operation := validOperation()
	operation.Name = "arbitrary-name"
	if _, err := (&TartHostOperationCustomValidator{}).ValidateCreate(t.Context(), operation); err == nil {
		t.Fatal("ValidateCreate() error = nil, want deterministic name error")
	}
}

func validMachine() *infrastructurev1beta1.TartMachine {
	return &infrastructurev1beta1.TartMachine{
		Spec: infrastructurev1beta1.TartMachineSpec{
			Image: infrastructurev1beta1.ImageSpec{
				Ref: "oci://registry.sample.walnuts.dev/tart/ubuntu@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			},
		},
	}
}

func validOperation() *infrastructurev1beta1.TartHostOperation {
	hostUID := types.UID("host-a-uid")
	activeName, err := operationdomain.ResourceName(string(hostUID))
	if err != nil {
		panic(err)
	}
	return &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{Name: activeName},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID:          "0197d640-8d00-7a65-b67f-3f7c42a6935f",
			Type:                 infrastructurev1beta1.OperationTypeProvision,
			HostRef:              infrastructurev1beta1.ResourceReference{Namespace: "default", Name: "host-a", UID: hostUID},
			MachineRef:           &infrastructurev1beta1.ResourceReference{Namespace: "default", Name: "machine-a", UID: types.UID("machine-a-uid")},
			PlanDigest:           "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			DesiredObjectsDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Deadline:             metav1.NewTime(time.Date(2026, 7, 4, 15, 0, 0, 0, time.UTC)),
		},
	}
}
