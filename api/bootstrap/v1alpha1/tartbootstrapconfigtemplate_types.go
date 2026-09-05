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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// TartBootstrapConfigTemplateResource describes the data needed to create a
// TartBootstrapConfig from a template.
type TartBootstrapConfigTemplateResource struct {
	// ObjectMeta is propagated to the generated BootstrapConfig and its Secret.
	ObjectMeta clusterv1.ObjectMeta                    `json:"metadata,omitempty,omitzero"`
	Spec       TartBootstrapConfigTemplateResourceSpec `json:"spec,omitempty,omitzero"`
}

// TartBootstrapConfigTemplateResourceSpec contains the immutable Secret input
// shared by concrete BootstrapConfigs created from the template.
type TartBootstrapConfigTemplateResourceSpec struct {
	// +optional
	ConfigSecretRef *corev1.LocalObjectReference `json:"configSecretRef,omitempty"`
}

// TartBootstrapConfigTemplateSpec defines the desired state of a TartBootstrapConfigTemplate.
type TartBootstrapConfigTemplateSpec struct {
	Template TartBootstrapConfigTemplateResource `json:"template"`
}

// +kubebuilder:object:root=true

// TartBootstrapConfigTemplate is the Schema for the tartbootstrapconfigtemplates API.
type TartBootstrapConfigTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TartBootstrapConfigTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// TartBootstrapConfigTemplateList contains a list of TartBootstrapConfigTemplate.
type TartBootstrapConfigTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartBootstrapConfigTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TartBootstrapConfigTemplate{}, &TartBootstrapConfigTemplateList{})
}
