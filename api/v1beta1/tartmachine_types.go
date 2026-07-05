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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

type DeletionPolicy string

const (
	DeletionPolicyWipeAll     DeletionPolicy = "WipeAll"
	DeletionPolicyRetainData  DeletionPolicy = "RetainData"
	DeletionPolicyRetainState DeletionPolicy = "RetainState"
)

type UpdateMode string

const (
	UpdateModeReplace UpdateMode = "Replace"
	UpdateModeInPlace UpdateMode = "InPlace"
)

type TartMachineSpec struct {
	// providerID must match the provider ID on the corresponding workload cluster Node.
	// This field is managed by the TartMachine controller.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	ProviderID string `json:"providerID,omitempty"`

	// image identifies the digest-pinned OS artifact to install.
	// +required
	Image ImageSpec `json:"image"`

	// platformProfile identifies the versioned platform configuration required by the machine.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	PlatformProfile string `json:"platformProfile"`

	// hostSelector selects TartHosts by label.
	// +optional
	HostSelector HostSelector `json:"hostSelector,omitempty,omitzero"`

	// updatePolicy selects replacement or experimental in-place updates.
	// +optional
	// +kubebuilder:default={mode:Replace}
	UpdatePolicy UpdatePolicy `json:"updatePolicy,omitempty"`

	// deletionPolicy controls how State and Data are handled when the machine is deleted.
	// +required
	// +kubebuilder:validation:Enum=WipeAll;RetainData;RetainState
	DeletionPolicy DeletionPolicy `json:"deletionPolicy"`
}

type ImageSpec struct {
	// ref is a digest-pinned OCI artifact reference.
	// +required
	// +kubebuilder:validation:Pattern=`^oci://[^@[:space:]]+@sha256:[0-9a-f]{64}$`
	Ref string `json:"ref"`
}

type HostSelector struct {
	// matchLabels selects hosts containing all listed labels.
	// +optional
	// +kubebuilder:validation:MaxProperties=64
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
}

type UpdatePolicy struct {
	// mode selects machine replacement or an experimental in-place update.
	// +optional
	// +kubebuilder:validation:Enum=Replace;InPlace
	// +kubebuilder:default=Replace
	Mode UpdateMode `json:"mode,omitempty"`
}

type TartMachineStatus struct {
	// initialization provides observations of the TartMachine initialization process.
	// +optional
	Initialization TartMachineInitializationStatus `json:"initialization,omitempty,omitzero"`

	// hostRef identifies the TartHost assigned to this machine.
	// +optional
	HostRef *ResourceReference `json:"hostRef,omitempty"`

	// operationRef identifies the current or most recent TartHostOperation.
	// +optional
	OperationRef *ResourceReference `json:"operationRef,omitempty"`

	// activeSlot is the OS slot currently reported by the host.
	// +optional
	// +kubebuilder:validation:Enum=A;B
	ActiveSlot OSSlot `json:"activeSlot,omitempty"`

	// installedImageDigest is the digest of the OS artifact running in activeSlot.
	// +optional
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	InstalledImageDigest string `json:"installedImageDigest,omitempty"`

	// failureDomain is the failure domain in which the machine was placed.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	FailureDomain string `json:"failureDomain,omitempty"`

	// addresses contains the addresses reported for the machine.
	// +optional
	Addresses clusterv1.MachineAddresses `json:"addresses,omitempty"`

	// conditions represent the current state of the TartMachine.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the last spec generation reconciled into status.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:validation:MinProperties=1
type TartMachineInitializationStatus struct {
	// provisioned is true when machine infrastructure provisioning has completed.
	// Once true, this value must never become false.
	// +optional
	Provisioned *bool `json:"provisioned,omitempty"`
}

type OSSlot string

const (
	OSSlotA OSSlot = "A"
	OSSlotB OSSlot = "B"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=tartmachines,scope=Namespaced,categories=cluster-api
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Provisioned",type=string,JSONPath=`.status.initialization.provisioned`,description="Infrastructure provisioned"
// +kubebuilder:printcolumn:name="Host",type=string,JSONPath=`.status.hostRef.name`,description="Assigned TartHost"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`

type TartMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              TartMachineSpec   `json:"spec"`
	Status            TartMachineStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

type TartMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartMachine `json:"items"`
}

func init() {
	registerKnownTypes(&TartMachine{}, &TartMachineList{})
}
