package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// TartClusterTemplateResource describes the data needed to create a TartCluster from a template.
type TartClusterTemplateResource struct {
	// ObjectMeta is propagated to the generated Cluster and its descendants.
	ObjectMeta clusterv1.ObjectMeta            `json:"metadata,omitempty,omitzero"`
	Spec       TartClusterTemplateResourceSpec `json:"spec,omitempty,omitzero"`
}

// TartClusterTemplateResourceSpec contains only fields that are meaningful for
// every concrete TartCluster created from the template. Cluster identity is
// always generated on the concrete resource after creation.
type TartClusterTemplateResourceSpec struct {
	// +optional
	UpdatePolicy TartUpdatePolicy `json:"updatePolicy,omitempty,omitzero"`
}

// TartClusterTemplateSpec defines the desired state of a TartClusterTemplate.
type TartClusterTemplateSpec struct {
	Template TartClusterTemplateResource `json:"template"`
}

// +kubebuilder:object:root=true
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1alpha1"

// TartClusterTemplate is the Schema for the tartclustertemplates API.
type TartClusterTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TartClusterTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// TartClusterTemplateList contains a list of TartClusterTemplate.
type TartClusterTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartClusterTemplate `json:"items"`
}
