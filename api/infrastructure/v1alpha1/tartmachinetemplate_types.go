package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// TartMachineTemplateResource describes the data needed to create a TartMachine from a template.
type TartMachineTemplateResource struct {
	// ObjectMetaは生成されるTartMachineのmetadataへ伝播する。CAPI Machineのmetadataは別途管理される。
	ObjectMeta clusterv1.ObjectMeta            `json:"metadata,omitempty,omitzero"`
	Spec       TartMachineTemplateResourceSpec `json:"spec,omitempty,omitzero"`
}

// TartMachineTemplateResourceSpec contains only fields that can be shared by
// all concrete Machines created from the template. Host claims and ProviderIDs
// are assigned on each concrete TartMachine and cannot be fixed here.
type TartMachineTemplateResourceSpec struct {
	// hostSelector narrows allocation without naming a particular Host.
	// +optional
	HostSelector *HostSelector `json:"hostSelector,omitempty"`

	// talosImage is the desired Talos OS version and system extension set.
	TalosImage TalosImage `json:"talosImage"`
}

// TartMachineTemplateSpec defines the desired state of a TartMachineTemplate.
type TartMachineTemplateSpec struct {
	Template TartMachineTemplateResource `json:"template"`
}

// +kubebuilder:object:root=true
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1alpha1"

// TartMachineTemplate is the Schema for the tartmachinetemplates API.
type TartMachineTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TartMachineTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// TartMachineTemplateList contains a list of TartMachineTemplate.
type TartMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartMachineTemplate `json:"items"`
}
