package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// TartMachineTemplateResourceはtemplateからTartMachineを作成するためのdataを定義する。
type TartMachineTemplateResource struct {
	// ObjectMetaは生成されるTartMachineのmetadataへ伝播する。CAPI Machineのmetadataは別途管理される。
	ObjectMeta clusterv1.ObjectMeta            `json:"metadata,omitempty,omitzero"`
	Spec       TartMachineTemplateResourceSpec `json:"spec,omitempty,omitzero"`
}

// TartMachineTemplateResourceSpecはtemplateから作成する全ての具体的なMachineで共有できるfieldだけを定義する。Host claimとProviderIDは各TartMachineに割り当てるため、ここでは固定できない。
type TartMachineTemplateResourceSpec struct {
	// hostSelectorは特定のHost名を指定せずallocation対象を絞り込む。
	// +optional
	HostSelector *HostSelector `json:"hostSelector,omitempty"`

	// imageはdesired Talos OS versionとsystem extension setを指定する。
	Image TalosImageSpec `json:"image"`
}

// TartMachineTemplateSpecはTartMachineTemplateのdesired stateを定義する。
type TartMachineTemplateSpec struct {
	Template TartMachineTemplateResource `json:"template"`
}

// +kubebuilder:object:root=true
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1alpha1"
// +kubebuilder:resource:categories=cluster-api,shortName=tmt

// TartMachineTemplateはtartmachinetemplates APIのschemaである。
type TartMachineTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TartMachineTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// TartMachineTemplateListはTartMachineTemplateのlistである。
type TartMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartMachineTemplate `json:"items"`
}
