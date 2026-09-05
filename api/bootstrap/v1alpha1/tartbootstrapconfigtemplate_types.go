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
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1alpha1"

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
