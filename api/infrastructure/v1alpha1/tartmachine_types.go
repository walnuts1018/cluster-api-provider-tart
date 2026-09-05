// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// TartMachine Condition types.
const (
	TartMachineReadyCondition          = "Ready"
	TartMachineTalosReachableCondition = "TalosReachable"
	TartMachineProvisionedCondition    = "Provisioned"
	TartMachineUpToDateCondition       = "UpToDate"
)

// TalosImage is the single source of truth for the Talos OS version and system
// extension set. The same schematic is used for both the boot asset and the installer
// image.
type TalosImage struct {
	// version is the desired Talos OS version.
	Version string `json:"version"`
	// schematicID is the Image Factory schematic identifier, which also determines the
	// installed system extension set.
	SchematicID string `json:"schematicID"`
}

// TartMachineSpec defines the desired state of a TartMachine.
type TartMachineSpec struct {
	// hostRef explicitly selects a TartHost. Immutable after the Host claim succeeds.
	// +optional
	HostRef *corev1.LocalObjectReference `json:"hostRef,omitempty"`

	// hostSelector deterministically narrows Host allocation when hostRef is not set.
	// Changing it after claim is a safe-stop, not an in-place update.
	// +optional
	HostSelector *HostSelector `json:"hostSelector,omitempty"`

	// talosImage is the desired Talos OS version and schematic identity.
	TalosImage TalosImage `json:"talosImage"`

	// providerID is derived deterministically from the claimed TartHost.spec.id as
	// tart://host/<TartHost.spec.id> once Host allocation succeeds.
	// +optional
	ProviderID string `json:"providerID,omitempty"`
}

// HostSelector narrows Host allocation to Hosts matching all given criteria.
type HostSelector struct {
	// +optional
	Architecture string `json:"architecture,omitempty"`
	// +optional
	FailureDomain string `json:"failureDomain,omitempty"`
	// +optional
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
}

// TartMachineStatus defines the observed state of a TartMachine.
type TartMachineStatus struct {
	// +optional
	Initialization TartMachineInitializationStatus `json:"initialization,omitempty"`

	// +optional
	Addresses clusterv1.MachineAddresses `json:"addresses,omitempty"`

	// +optional
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
type TartMachineInitializationStatus struct {
	// +optional
	Provisioned bool `json:"provisioned,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
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

func init() {
	SchemeBuilder.Register(&TartMachine{}, &TartMachineList{})
}
