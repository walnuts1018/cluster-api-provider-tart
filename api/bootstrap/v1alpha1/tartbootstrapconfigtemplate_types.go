package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// TartBootstrapConfigTemplateResourceはtemplateからTartBootstrapConfigを作成するためのdataを定義する。
type TartBootstrapConfigTemplateResource struct {
	// ObjectMetaは生成されるBootstrapConfigとそのSecretへ伝播する。
	ObjectMeta clusterv1.ObjectMeta                    `json:"metadata,omitempty,omitzero"`
	Spec       TartBootstrapConfigTemplateResourceSpec `json:"spec,omitempty,omitzero"`
}

// TartBootstrapConfigTemplateResourceSpecはtemplateから作成される具体的なBootstrapConfigで共有するimmutable Secret inputを定義する。
type TartBootstrapConfigTemplateResourceSpec struct {
	// +optional
	ConfigPatchesSecretRef *corev1.LocalObjectReference `json:"configPatchesSecretRef,omitempty"`

	// updatePolicyは生成されるTartBootstrapConfigのconfiguration update policyを共有する。
	// +optional
	UpdatePolicy TartBootstrapConfigUpdatePolicy `json:"updatePolicy,omitempty,omitzero"`
}

// TartBootstrapConfigTemplateSpecはTartBootstrapConfigTemplateのdesired stateを定義する。
type TartBootstrapConfigTemplateSpec struct {
	Template TartBootstrapConfigTemplateResource `json:"template"`
}

// +kubebuilder:object:root=true
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1alpha1"
// +kubebuilder:resource:categories=cluster-api,shortName=tbcfgt

// TartBootstrapConfigTemplateはtartbootstrapconfigtemplates APIのschemaである。
type TartBootstrapConfigTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TartBootstrapConfigTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// TartBootstrapConfigTemplateListはTartBootstrapConfigTemplateのlistである。
type TartBootstrapConfigTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartBootstrapConfigTemplate `json:"items"`
}
