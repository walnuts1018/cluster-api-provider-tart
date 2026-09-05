package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
)

// TartMachine Condition types.
const (
	TartMachineReadyCondition          = "Ready"
	TartMachineTalosReachableCondition = "TalosReachable"
	TartMachineProvisionedCondition    = "Provisioned"
	TartMachineTalosUpToDateCondition  = "TalosUpToDate"
)

// TalosImageSpec is the single source of truth for the Talos OS version and system
// extension set. The same schematic is used for both the boot asset and the installer
// image.
type TalosImageSpec struct {
	// version is the desired Talos OS version. It must follow semantic versioning with a leading "v".
	// +kubebuilder:validation:Pattern="^v[0-9]+\\.[0-9]+\\.[0-9]+.*$"
	Version string `json:"version"`
	// schematicID is the Image Factory schematic identifier, which also determines the
	// installed system extension set.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	SchematicID string `json:"schematicID"`
}

// TartMachineSpec defines the desired state of a TartMachine.
// Field classification:
// - hostRef: initial-only / user-owned (immutable after claim)
// - hostSelector: initial-only / user-owned (safe-stop after claim)
// - image: mutable / Update Extension-owned lifecycle (in-place Talos updates are handled via Update Extension, not by normal TartMachine reconciler)
// - providerID: controller-written
//
// +kubebuilder:validation:XValidation:rule="!(has(self.hostRef) && has(self.hostSelector))",message="hostRef and hostSelector are mutually exclusive"
type TartMachineSpec struct {
	// hostRef explicitly selects a TartHost (initial-only, user-owned). Immutable after Host claim succeeds.
	// Mutually exclusive with hostSelector.
	// +optional
	HostRef *corev1.LocalObjectReference `json:"hostRef,omitempty"`

	// hostSelector deterministically narrows Host allocation when hostRef is not set (initial-only, user-owned).
	// Changing it after claim is a safe-stop, not an in-place update.
	// Mutually exclusive with hostRef.
	// +optional
	HostSelector *HostSelector `json:"hostSelector,omitempty"`

	// image is the desired Talos OS version and schematic identity (mutable, Update Extension-owned).
	// Normal TartMachine reconciler does not trigger Talos upgrades on live nodes directly;
	// upgrades are driven by the CAPI Update Extension lifecycle (CanUpdateMachine -> UpdateMachine).
	Image TalosImageSpec `json:"image"`

	// providerID is derived deterministically from the claimed TartHost.spec.hostID as
	// tart://host/<TartHost.spec.hostID> once Host allocation succeeds (controller-written).
	// Invariant: TartHost.spec.hostID -> tart://host/<hostID> -> TartMachine.spec.providerID == Node.spec.providerID.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	// +optional
	// +kubebuilder:validation:Type=string
	ProviderID hostdomain.ProviderID `json:"providerID,omitempty,omitzero"`
}

// HostSelector narrows Host allocation to Hosts matching all given criteria.
type HostSelector struct {
	// +optional
	Architecture string `json:"architecture,omitempty"`
	// +optional
	Selector metav1.LabelSelector `json:"selector,omitempty,omitzero"`
}

// TartMachineStatus defines the observed state of a TartMachine.
type TartMachineStatus struct {
	// +optional
	Initialization TartMachineInitializationStatus `json:"initialization,omitempty,omitzero"`

	// +optional
	Addresses clusterv1.MachineAddresses `json:"addresses,omitempty"`

	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	FailureDomain string `json:"failureDomain,omitempty"`

	// hostRef is the observed Host binding, i.e. the Host currently claimed via
	// TartHost.spec.consumerRef for this Machine.
	// +optional
	HostRef *corev1.LocalObjectReference `json:"hostRef,omitempty"`

	// talosVersion is the observed actual Talos OS version.
	// +optional
	TalosVersion string `json:"talosVersion,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// TartMachineInitializationStatus tracks initial provisioning milestones.
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

// TartMachine is the Schema for the tartmachines API.
type TartMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TartMachineSpec   `json:"spec,omitempty"`
	Status TartMachineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TartMachineList contains a list of TartMachine.
type TartMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartMachine `json:"items"`
}
