/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TartMachineBootstrapFormat selects how bootstrap data is exposed to the booted OS or installer.
type TartMachineBootstrapFormat string

const (
	// TartMachineBootstrapFormatTalos serves bootstrap data as a single Talos machine config.
	TartMachineBootstrapFormatTalos TartMachineBootstrapFormat = "Talos"
	// TartMachineBootstrapFormatNoCloud serves bootstrap data through cloud-init NoCloud files.
	TartMachineBootstrapFormatNoCloud TartMachineBootstrapFormat = "NoCloud"
	// TartMachineBootstrapFormatPreseed serves bootstrap data as a Debian Installer preseed file.
	TartMachineBootstrapFormatPreseed TartMachineBootstrapFormat = "Preseed"
	// TartMachineBootstrapFormatRaw leaves bootstrap kernel parameters fully user-managed.
	TartMachineBootstrapFormatRaw TartMachineBootstrapFormat = "Raw"
)

// TartMachineBootstrapSpec defines how bootstrap data is served to the machine.
type TartMachineBootstrapSpec struct {
	// format selects how bootstrap data is exposed to the booted OS or installer.
	// Defaults to Talos when omitted.
	// +optional
	// +kubebuilder:validation:Enum=Talos;NoCloud;Preseed;Raw
	Format TartMachineBootstrapFormat `json:"format,omitempty"`
}

// TartMachineSpec defines the desired state of TartMachine
type TartMachineSpec struct {
	// providerID is the infrastructure provider ID reported back to the CAPI Machine.
	// +optional
	ProviderID string `json:"providerID,omitempty"`

	// failureDomain specifies the failure domain for the machine.
	// Must comply with the failure domain specified in the core Machine object.
	// +optional
	FailureDomain string `json:"failureDomain,omitempty"`

	// image is the boot OS image URL or a path served by the assets server.
	// +kubebuilder:validation:MinLength=1
	// +required
	Image string `json:"image"`

	// kernelParams are passed from iPXE to the OS kernel.
	// +optional
	// +kubebuilder:validation:MaxItems=100
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=2048
	KernelParams []string `json:"kernelParams,omitempty"`

	// initrd is the boot initrd image URL or a path served by the assets server.
	// +optional
	// +kubebuilder:validation:MinLength=1
	Initrd string `json:"initrd,omitempty"`

	// bootstrap configures how bootstrap data is passed to the booted OS or installer.
	// +optional
	Bootstrap TartMachineBootstrapSpec `json:"bootstrap,omitempty"`
}

// TartMachineStatus defines the observed state of TartMachine.
type TartMachineStatus struct {
	// ready indicates whether the infrastructure resource is ready for the CAPI Machine.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// initialization holds the initialization state of the TartMachine.
	// +optional
	Initialization TartMachineInitialization `json:"initialization,omitempty"`

	// hostRef references the assigned TartHost.
	// +optional
	HostRef *corev1.ObjectReference `json:"hostRef,omitempty"`

	// bootstrapSecretName is the Secret name that stores bootstrap data served once to the host.
	// +optional
	BootstrapSecretName string `json:"bootstrapSecretName,omitempty"`

	// provisioningStartTime is the start of the WoL and metadata access window.
	// +optional
	ProvisioningStartTime *metav1.Time `json:"provisioningStartTime,omitempty"`

	// tokenExpiresAt is the deadline for serving bootstrap data.
	// +optional
	TokenExpiresAt *metav1.Time `json:"tokenExpiresAt,omitempty"`

	// addresses holds the IP addresses of the provisioned machine.
	// +optional
	Addresses []TartMachineAddress `json:"addresses,omitempty"`

	// observedGeneration is the last spec generation reconciled into status.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the TartMachine resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// consumedBootstrapTokenHash stores the SHA-256 hash of the consumed bootstrap token for non-secret NoCloud metadata validation.
	// +optional
	ConsumedBootstrapTokenHash string `json:"consumedBootstrapTokenHash,omitempty"`
}

// TartMachineInitialization defines the initialization state of TartMachine.
type TartMachineInitialization struct {
	// provisioned indicates that the infrastructure is provisioned and ready.
	// +optional
	Provisioned bool `json:"provisioned,omitempty"`
}

// TartMachineAddress defines the IP address of the machine.
type TartMachineAddress struct {
	// address is the IP address of the machine.
	// +optional
	Address string `json:"address,omitempty"`

	// type is the type of the address.
	// +optional
	Type corev1.NodeAddressType `json:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.ready`,description="Ready status"
// +kubebuilder:printcolumn:name="Host",type=string,JSONPath=`.status.hostRef.name`,description="Assigned TartHost"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`

// TartMachine is the Schema for the tartmachines API
type TartMachine struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of TartMachine
	// +required
	Spec TartMachineSpec `json:"spec"`

	// status defines the observed state of TartMachine
	// +optional
	Status TartMachineStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TartMachineList contains a list of TartMachine
type TartMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TartMachine `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TartMachine{}, &TartMachineList{})
}
