package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// TartControlPlaneTemplateResourceはtemplateからTartControlPlaneを作成するためのdataを定義する。
type TartControlPlaneTemplateResource struct {
	// ObjectMetaは生成されるTartControlPlaneのmetadataへ伝播する。各control-plane Machineのmetadataは内側のmachineTemplate.metadataで指定する。
	ObjectMeta clusterv1.ObjectMeta                 `json:"metadata,omitempty,omitzero"`
	Spec       TartControlPlaneTemplateResourceSpec `json:"spec,omitempty,omitzero"`
}

// TartControlPlaneTemplateResourceSpecはClusterClassが計算または注入するversion、replicas、infrastructureRefなどの値を含めない。
type TartControlPlaneTemplateResourceSpec struct {
	// +optional
	MachineTemplate TartControlPlaneTemplateMachineTemplate `json:"machineTemplate,omitempty,omitzero"`

	// +optional
	BootstrapConfigTemplateRef *clusterv1.ContractVersionedObjectReference `json:"bootstrapConfigTemplateRef,omitempty"`
}

// TartControlPlaneTemplateMachineTemplateはcontrol-plane Machine templateのtemplate専用形式である。infrastructure referenceはClusterClassまたは具体的なTartControlPlaneが注入する。
type TartControlPlaneTemplateMachineTemplate struct {
	// +optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// +optional
	Spec TartControlPlaneTemplateMachineTemplateSpec `json:"spec,omitempty,omitzero"`
}

// TartControlPlaneTemplateMachineTemplateSpecはinfrastructure template referenceを固定せずに共有できるfieldを定義する。
type TartControlPlaneTemplateMachineTemplateSpec struct {
	// readinessGatesはMachine readinessを評価する追加Conditionを指定する。
	// +optional
	ReadinessGates []clusterv1.MachineReadinessGate `json:"readinessGates,omitempty,omitzero"`

	// taintsは作成したMachineへ適用するnode taintを指定する。
	// +optional
	Taints []clusterv1.MachineTaint `json:"taints,omitempty,omitzero"`

	// +optional
	Deletion TartControlPlaneMachineTemplateDeletionSpec `json:"deletion,omitempty,omitzero"`
}

// TartControlPlaneTemplateSpecはTartControlPlaneTemplateのdesired stateを定義する。
type TartControlPlaneTemplateSpec struct {
	Template TartControlPlaneTemplateResource `json:"template"`
}

// +kubebuilder:object:root=true
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1alpha1"
// +kubebuilder:resource:categories=cluster-api,shortName=tcpt

// TartControlPlaneTemplateはtartcontrolplanetemplates APIのschemaである。
type TartControlPlaneTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TartControlPlaneTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// TartControlPlaneTemplateListはTartControlPlaneTemplateのlistである。
type TartControlPlaneTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartControlPlaneTemplate `json:"items"`
}
