package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// TartClusterTemplateResourceはtemplateからTartClusterを作成するためのdataを定義する。
type TartClusterTemplateResource struct {
	// ObjectMetaは生成されるClusterとその子リソースへ伝播する。
	ObjectMeta clusterv1.ObjectMeta            `json:"metadata,omitempty,omitzero"`
	Spec       TartClusterTemplateResourceSpec `json:"spec,omitempty,omitzero"`
}

// TartClusterTemplateResourceSpecはtemplateから作成される全ての具体的なTartClusterで意味を持つfieldだけを定義する。Cluster identityは具体的なresourceの作成後に常に生成する。
type TartClusterTemplateResourceSpec struct {
	// +optional
	UpdatePolicy TartUpdatePolicy `json:"updatePolicy,omitempty,omitzero"`
}

// TartClusterTemplateSpecはTartClusterTemplateのdesired stateを定義する。
type TartClusterTemplateSpec struct {
	Template TartClusterTemplateResource `json:"template"`
}

// +kubebuilder:object:root=true
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1alpha1"
// +kubebuilder:resource:categories=cluster-api,shortName=tclt

// TartClusterTemplateはtartclustertemplates APIのschemaである。
type TartClusterTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TartClusterTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// TartClusterTemplateListはTartClusterTemplateのlistである。
type TartClusterTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartClusterTemplate `json:"items"`
}
