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

// TartClusterTemplateSpec defines the desired state of TartClusterTemplate.
type TartClusterTemplateSpec struct {
	// template is the TartCluster object template used by Cluster API controllers.
	// +required
	Template TartClusterTemplateResource `json:"template"`
}

// TartClusterTemplateResource describes the TartCluster created from a template.
type TartClusterTemplateResource struct {
	// metadata is applied to the generated TartCluster.
	// +optional
	ObjectMeta metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of the generated TartCluster.
	// +required
	Spec TartClusterSpec `json:"spec"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=tartclustertemplates,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion

// TartClusterTemplate is the Schema for the tartclustertemplates API.
type TartClusterTemplate struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of TartClusterTemplate.
	// +required
	Spec TartClusterTemplateSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// TartClusterTemplateList contains a list of TartClusterTemplate.
type TartClusterTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TartClusterTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TartClusterTemplate{}, &TartClusterTemplateList{})
}
