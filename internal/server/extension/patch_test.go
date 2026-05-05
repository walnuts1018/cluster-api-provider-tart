/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package extension

import (
	"testing"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
)

func TestHasRollingUpdateRequiredChanges(t *testing.T) {
	baseSpec := infrastructurev1alpha1.TartMachineSpec{
		Image:        "ubuntu:22.04",
		Initrd:       "/initrd.img",
		KernelParams: []string{"param1=value1", "param2=value2"},
	}

	tests := []struct {
		name     string
		current  infrastructurev1alpha1.TartMachineSpec
		desired  infrastructurev1alpha1.TartMachineSpec
		expected bool
	}{
		{
			name:     "identical specs - no rolling update required",
			current:  baseSpec,
			desired:  baseSpec,
			expected: false,
		},
		{
			name:    "image changed - rolling update required",
			current: baseSpec,
			desired: infrastructurev1alpha1.TartMachineSpec{
				Image:        "ubuntu:24.04",
				Initrd:       "/initrd.img",
				KernelParams: []string{"param1=value1", "param2=value2"},
			},
			expected: true,
		},
		{
			name:    "initrd changed - rolling update required",
			current: baseSpec,
			desired: infrastructurev1alpha1.TartMachineSpec{
				Image:        "ubuntu:22.04",
				Initrd:       "/new-initrd.img",
				KernelParams: []string{"param1=value1", "param2=value2"},
			},
			expected: true,
		},
		{
			name:    "kernel params changed - rolling update required",
			current: baseSpec,
			desired: infrastructurev1alpha1.TartMachineSpec{
				Image:        "ubuntu:22.04",
				Initrd:       "/initrd.img",
				KernelParams: []string{"param1=value1", "param2=value3"},
			},
			expected: true,
		},
		{
			name:    "kernel params count changed - rolling update required",
			current: baseSpec,
			desired: infrastructurev1alpha1.TartMachineSpec{
				Image:        "ubuntu:22.04",
				Initrd:       "/initrd.img",
				KernelParams: []string{"param1=value1"},
			},
			expected: true,
		},
		{
			name: "only providerID changed - no rolling update required",
			current: infrastructurev1alpha1.TartMachineSpec{
				Image:        "ubuntu:22.04",
				Initrd:       "/initrd.img",
				KernelParams: []string{"param1=value1"},
				ProviderID:   "old-id",
			},
			desired: infrastructurev1alpha1.TartMachineSpec{
				Image:        "ubuntu:22.04",
				Initrd:       "/initrd.img",
				KernelParams: []string{"param1=value1"},
				ProviderID:   "new-id",
			},
			expected: false,
		},
		{
			name: "only failureDomain changed - no rolling update required",
			current: infrastructurev1alpha1.TartMachineSpec{
				Image:         "ubuntu:22.04",
				Initrd:        "/initrd.img",
				KernelParams:  []string{"param1=value1"},
				FailureDomain: "dc1",
			},
			desired: infrastructurev1alpha1.TartMachineSpec{
				Image:         "ubuntu:22.04",
				Initrd:        "/initrd.img",
				KernelParams:  []string{"param1=value1"},
				FailureDomain: "dc2",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasRollingUpdateRequiredChanges(tt.current, tt.desired)
			if got != tt.expected {
				t.Errorf("hasRollingUpdateRequiredChanges() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCanUpdateInPlace(t *testing.T) {
	tests := []struct {
		name     string
		current  *infrastructurev1alpha1.TartMachine
		desired  *infrastructurev1alpha1.TartMachine
		expected bool
	}{
		{
			name:     "nil current - returns true",
			current:  nil,
			desired:  &infrastructurev1alpha1.TartMachine{},
			expected: true,
		},
		{
			name:     "nil desired - returns true",
			current:  &infrastructurev1alpha1.TartMachine{},
			desired:  nil,
			expected: true,
		},
		{
			name:     "both nil - returns true",
			current:  nil,
			desired:  nil,
			expected: true,
		},
		{
			name: "same specs - can update in place",
			current: &infrastructurev1alpha1.TartMachine{
				Spec: infrastructurev1alpha1.TartMachineSpec{
					Image: "ubuntu:22.04",
				},
			},
			desired: &infrastructurev1alpha1.TartMachine{
				Spec: infrastructurev1alpha1.TartMachineSpec{
					Image: "ubuntu:22.04",
				},
			},
			expected: true,
		},
		{
			name: "different image - cannot update in place",
			current: &infrastructurev1alpha1.TartMachine{
				Spec: infrastructurev1alpha1.TartMachineSpec{
					Image: "ubuntu:22.04",
				},
			},
			desired: &infrastructurev1alpha1.TartMachine{
				Spec: infrastructurev1alpha1.TartMachineSpec{
					Image: "ubuntu:24.04",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanUpdateInPlace(tt.current, tt.desired)
			if got != tt.expected {
				t.Errorf("CanUpdateInPlace() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestPatchTartMachineSpec(t *testing.T) {
	tests := []struct {
		name    string
		current *infrastructurev1alpha1.TartMachine
		desired *infrastructurev1alpha1.TartMachine
		wantNil bool
	}{
		{
			name: "no changes - returns nil",
			current: &infrastructurev1alpha1.TartMachine{
				Spec: infrastructurev1alpha1.TartMachineSpec{
					Image: "ubuntu:22.04",
				},
			},
			desired: &infrastructurev1alpha1.TartMachine{
				Spec: infrastructurev1alpha1.TartMachineSpec{
					Image: "ubuntu:22.04",
				},
			},
			wantNil: true,
		},
		{
			name: "only providerID changed - returns patch",
			current: &infrastructurev1alpha1.TartMachine{
				Spec: infrastructurev1alpha1.TartMachineSpec{
					Image:      "ubuntu:22.04",
					ProviderID: "old-id",
				},
			},
			desired: &infrastructurev1alpha1.TartMachine{
				Spec: infrastructurev1alpha1.TartMachineSpec{
					Image:      "ubuntu:22.04",
					ProviderID: "new-id",
				},
			},
			wantNil: false,
		},
		{
			name: "image changed - returns empty TartMachine (rolling update signal)",
			current: &infrastructurev1alpha1.TartMachine{
				Spec: infrastructurev1alpha1.TartMachineSpec{
					Image: "ubuntu:22.04",
				},
			},
			desired: &infrastructurev1alpha1.TartMachine{
				Spec: infrastructurev1alpha1.TartMachineSpec{
					Image: "ubuntu:24.04",
				},
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := patchTartMachineSpec(tt.current, tt.desired)
			if err != nil {
				t.Fatalf("patchTartMachineSpec() error = %v", err)
			}
			if (got == nil) != tt.wantNil {
				t.Errorf("patchTartMachineSpec() got = %v, wantNil = %v", got, tt.wantNil)
			}
		})
	}
}
