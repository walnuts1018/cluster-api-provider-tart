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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

type TartMachineTemplateSpec struct {
	// template is the TartMachine object template used by Cluster API controllers.
	// +required
	Template TartMachineTemplateResource `json:"template"`
}

type TartMachineTemplateResource struct {
	// metadata is applied to the generated TartMachine.
	// +optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines fields copied to a generated TartMachine.
	// +required
	Spec TartMachineTemplateResourceSpec `json:"spec"`
}

type TartMachineTemplateResourceSpec struct {
	// image identifies the digest-pinned OS artifact to install.
	// +required
	Image ImageSpec `json:"image"`

	// platformProfile identifies the versioned platform configuration required by the machine.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	PlatformProfile string `json:"platformProfile"`

	// hostSelector selects TartHosts by label.
	// +optional
	HostSelector HostSelector `json:"hostSelector,omitempty,omitzero"`

	// updatePolicy selects replacement or experimental in-place updates.
	// +optional
	// +kubebuilder:default={mode:Replace}
	UpdatePolicy UpdatePolicy `json:"updatePolicy,omitempty"`

	// deletionPolicy controls how State and Data are handled when the machine is deleted.
	// +required
	// +kubebuilder:validation:Enum=WipeAll;RetainData;RetainState
	DeletionPolicy DeletionPolicy `json:"deletionPolicy"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=tartmachinetemplates,scope=Namespaced,categories=cluster-api

type TartMachineTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              TartMachineTemplateSpec `json:"spec"`
}

// +kubebuilder:object:root=true

type TartMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartMachineTemplate `json:"items"`
}

func init() {
	registerKnownTypes(&TartMachineTemplate{}, &TartMachineTemplateList{})
}
