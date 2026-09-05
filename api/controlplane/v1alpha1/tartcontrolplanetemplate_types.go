package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// TartControlPlaneTemplateResource describes the data needed to create a
// TartControlPlane from a template.
type TartControlPlaneTemplateResource struct {
	// ObjectMetaは生成されるTartControlPlaneのmetadataへ伝播する。各control-plane Machineのmetadataは内側のmachineTemplate.metadataで指定する。
	ObjectMeta clusterv1.ObjectMeta                 `json:"metadata,omitempty,omitzero"`
	Spec       TartControlPlaneTemplateResourceSpec `json:"spec,omitempty,omitzero"`
}

// TartControlPlaneTemplateResourceSpec omits values calculated or injected by
// ClusterClass, such as version, replicas, and infrastructureRef.
type TartControlPlaneTemplateResourceSpec struct {
	// +optional
	MachineTemplate TartControlPlaneTemplateMachineTemplate `json:"machineTemplate,omitempty,omitzero"`

	// +optional
	BootstrapConfigTemplate *corev1.ObjectReference `json:"bootstrapConfigTemplate,omitempty"`
}

// TartControlPlaneTemplateMachineTemplate is the template-specific form of the
// control-plane Machine template. The infrastructure reference is injected by
// ClusterClass or the concrete TartControlPlane.
type TartControlPlaneTemplateMachineTemplate struct {
	// +optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// +optional
	Spec TartControlPlaneTemplateMachineTemplateSpec `json:"spec,omitempty,omitzero"`
}

// TartControlPlaneTemplateMachineTemplateSpec contains fields that can be
// shared without fixing the infrastructure template reference.
type TartControlPlaneTemplateMachineTemplateSpec struct {
	// +optional
	Deletion TartControlPlaneMachineTemplateDeletionSpec `json:"deletion,omitempty,omitzero"`
}

// TartControlPlaneTemplateSpec defines the desired state of a TartControlPlaneTemplate.
type TartControlPlaneTemplateSpec struct {
	Template TartControlPlaneTemplateResource `json:"template"`
}

// +kubebuilder:object:root=true
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1alpha1"

// TartControlPlaneTemplate is the Schema for the tartcontrolplanetemplates API.
type TartControlPlaneTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TartControlPlaneTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// TartControlPlaneTemplateList contains a list of TartControlPlaneTemplate.
type TartControlPlaneTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartControlPlaneTemplate `json:"items"`
}
