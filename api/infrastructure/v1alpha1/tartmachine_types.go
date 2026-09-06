package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
)

// TartMachineのCondition typeを定義する。
const (
	TartMachineReadyCondition          = "Ready"
	TartMachineTalosReachableCondition = "TalosReachable"
	TartMachineProvisionedCondition    = "Provisioned"
	TartMachineTalosUpToDateCondition  = "TalosUpToDate"
)

// TalosImageSpecはTalos OS versionとsystem extension setの唯一の正本である。同じschematicをboot assetとinstaller imageの双方に使用する。
type TalosImageSpec struct {
	// versionはdesired Talos OS versionである。先頭にvを付けたsemantic versioning(prerelease/build metadataは任意)に従わなければならない。
	// TartはTalos v1.14.0以降の multi-document machine configurationにのみ対応するため、v1.14.0未満は拒否する。
	// このfieldはTartMachineTemplateからも共有されるため、この検証はTartMachineTemplate経由の作成/更新にも適用される。
	// +kubebuilder:validation:Pattern="^v[0-9]+\\.[0-9]+\\.[0-9]+(-[0-9A-Za-z.-]+)?(\\+[0-9A-Za-z.-]+)?$"
	// +kubebuilder:validation:XValidation:rule="int(self.split('.')[0].substring(1)) > 1 || (int(self.split('.')[0].substring(1)) == 1 && int(self.split('.')[1]) >= 14)",message="Talos version must be v1.14.0 or later"
	Version string `json:"version"`
	// schematicIDはImage Factory schematic identifierであり、installされるsystem extension setも決定する。
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern="^[A-Za-z0-9][A-Za-z0-9._-]*$"
	SchematicID string `json:"schematicID"`
}

// TartMachineSpecはTartMachineのdesired stateを定義する。field classificationは、hostRefがinitial-onlyかつuser-ownedでclaim後immutable、hostSelectorがinitial-onlyかつuser-ownedでclaim後の変更はsafe-stop、imageがmutableかつUpdate Extension-owned lifecycle、providerIDがcontroller-writtenである。
// +kubebuilder:validation:XValidation:rule="!(has(self.hostRef) && has(self.hostSelector))",message="hostRef and hostSelector are mutually exclusive"
type TartMachineSpec struct {
	// hostRefはTartHostを明示的に選択する(initial-onlyかつuser-owned)。Host claim成功後はimmutableであり、hostSelectorとは相互排他的である。
	// +optional
	HostRef *corev1.LocalObjectReference `json:"hostRef,omitempty"`

	// hostSelectorはhostRef未指定時にHost allocation対象を決定論的に絞り込む(initial-onlyかつuser-owned)。claim後の変更はin-place updateではなくsafe-stopとして扱い、hostRefとは相互排他的である。
	// +optional
	HostSelector *HostSelector `json:"hostSelector,omitempty"`

	// imageはdesired Talos OS versionとschematic identityを指定する(mutableかつUpdate Extension-owned)。通常のTartMachine reconcilerは稼働中nodeのTalos upgradeを直接開始せず、CAPI Update Extension lifecycle(CanUpdateMachineからUpdateMachine)がupgradeを実行する。
	Image TalosImageSpec `json:"image"`

	// providerIDはHost allocation成功後にclaimed TartHost.spec.hostIDからtart://host/<TartHost.spec.hostID>として決定論的に導出する(controller-written)。不変条件はTartHost.spec.hostIDからtart://host/<hostID>を経てTartMachine.spec.providerIDとNode.spec.providerIDが一致することである。
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	// +optional
	// +kubebuilder:validation:Type=string
	ProviderID hostdomain.ProviderID `json:"providerID,omitempty,omitzero"`
}

// HostSelectorは指定された全ての条件に一致するHostへallocation対象を絞り込む。
type HostSelector struct {
	// +optional
	Architecture string `json:"architecture,omitempty"`
	// +optional
	Selector metav1.LabelSelector `json:"selector,omitempty,omitzero"`
}

// TartMachineStatusはTartMachineのobserved stateを定義する。
type TartMachineStatus struct {
	// +optional
	Initialization TartMachineInitializationStatus `json:"initialization,omitempty,omitzero"`

	// +optional
	Addresses clusterv1.MachineAddresses `json:"addresses,omitempty"`

	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	FailureDomain string `json:"failureDomain,omitempty"`

	// hostRefはobserved Host bindingであり、このMachineのためにTartHost.spec.consumerRefを通じて現在claimしているHostを示す。
	// +optional
	HostRef *corev1.LocalObjectReference `json:"hostRef,omitempty"`

	// talosVersionは観測した実際のTalos OS versionである。
	// +optional
	TalosVersion string `json:"talosVersion,omitempty"`

	// talosSchematicIDは観測したImage Factory schematic identityである。versionが同じでもsystem extension setがrollbackしたことを検知するために保持する。
	// +optional
	TalosSchematicID string `json:"talosSchematicID,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// TartMachineInitializationStatusは初回provisioningの到達点を追跡する。
// +kubebuilder:validation:MinProperties=1
type TartMachineInitializationStatus struct {
	// +optional
	Provisioned *bool `json:"provisioned,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1alpha1"
// +kubebuilder:resource:categories=cluster-api,shortName=tm
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="ProviderID",type=string,JSONPath=".spec.providerID"

// TartMachineはtartmachines APIのschemaである。
type TartMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TartMachineSpec   `json:"spec,omitempty"`
	Status TartMachineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TartMachineListはTartMachineのlistである。
type TartMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartMachine `json:"items"`
}
