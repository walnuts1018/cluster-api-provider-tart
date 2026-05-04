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

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// TartMachineTemplateSpec defines the desired state of TartMachineTemplate.
type TartMachineTemplateSpec struct {
	// template is the TartMachine object template used by Cluster API controllers.
	// +required
	Template TartMachineTemplateResource `json:"template"`
}

// TartMachineTemplateResource describes the TartMachine created from a template.
type TartMachineTemplateResource struct {
	// metadata is applied to the generated TartMachine.
	// +optional
	ObjectMeta metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of the generated TartMachine.
	// +required
	Spec TartMachineSpec `json:"spec"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=tartmachinetemplates,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion

// TartMachineTemplate is the Schema for the tartmachinetemplates API.
type TartMachineTemplate struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of TartMachineTemplate.
	// +required
	Spec TartMachineTemplateSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// TartMachineTemplateList contains a list of TartMachineTemplate.
type TartMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TartMachineTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TartMachineTemplate{}, &TartMachineTemplateList{})
}
