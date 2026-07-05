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

package v1alpha1

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
)

func TestTartMachineTemplateRegistersInfrastructureTemplateKind(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}

	gvks, _, err := scheme.ObjectKinds(&TartMachineTemplate{})
	if err != nil {
		t.Fatalf("ObjectKinds() error = %v", err)
	}
	if len(gvks) != 1 {
		t.Fatalf("ObjectKinds() returned %d GVKs, want 1", len(gvks))
	}

	if got, want := gvks[0].Kind, "TartMachineTemplate"; got != want {
		t.Fatalf("kind = %q, want %q", got, want)
	}
}

func TestTartMachineTemplateCarriesTartMachineSpec(t *testing.T) {
	template := TartMachineTemplate{
		Spec: TartMachineTemplateSpec{
			Template: TartMachineTemplateResource{
				Spec: TartMachineSpec{
					Image:        "https://assets.hoge.test.walnuts.dev/ubuntu/vmlinuz",
					Initrd:       "https://assets.hoge.test.walnuts.dev/ubuntu/initrd",
					KernelParams: []string{"console=tty0", "autoinstall"},
					Bootstrap: TartMachineBootstrapSpec{
						Format: TartMachineBootstrapFormatNoCloud,
					},
				},
			},
		},
	}

	if got, want := template.Spec.Template.Spec.Image, "https://assets.hoge.test.walnuts.dev/ubuntu/vmlinuz"; got != want {
		t.Fatalf("image = %q, want %q", got, want)
	}
	if got, want := template.Spec.Template.Spec.Initrd, "https://assets.hoge.test.walnuts.dev/ubuntu/initrd"; got != want {
		t.Fatalf("initrd = %q, want %q", got, want)
	}
	if got, want := template.Spec.Template.Spec.KernelParams[1], "autoinstall"; got != want {
		t.Fatalf("kernelParams[1] = %q, want %q", got, want)
	}
	if got, want := template.Spec.Template.Spec.Bootstrap.Format, TartMachineBootstrapFormatNoCloud; got != want {
		t.Fatalf("bootstrap.format = %q, want %q", got, want)
	}
}
