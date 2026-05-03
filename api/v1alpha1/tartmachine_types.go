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

// TartMachineSpec defines the desired state of TartMachine
type TartMachineSpec struct {
	// providerID is the infrastructure provider ID reported back to the CAPI Machine.
	// +optional
	ProviderID string `json:"providerID,omitempty"`

	// image is the boot OS image URL or a path served by the assets server.
	// +kubebuilder:validation:MinLength=1
	// +required
	Image string `json:"image"`

	// kernelParams are passed from iPXE to the OS kernel.
	// +optional
	KernelParams []string `json:"kernelParams,omitempty"`

	// initrd is the boot initrd image URL or a path served by the assets server.
	// +optional
	Initrd string `json:"initrd,omitempty"`
}

// TartMachineStatus defines the observed state of TartMachine.
type TartMachineStatus struct {
	// ready indicates whether the infrastructure resource is ready for the CAPI Machine.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// hostRef references the assigned TartHost.
	// +optional
	HostRef *corev1.ObjectReference `json:"hostRef,omitempty"`

	// bootstrapToken is an unguessable one-time token for serving bootstrap data.
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9]+$`
	// +kubebuilder:validation:MinLength=64
	// +kubebuilder:validation:MaxLength=64
	// +optional
	BootstrapToken string `json:"bootstrapToken,omitempty"`

	// bootstrapSecretName is the Secret name that stores bootstrap data served once to the host.
	// +optional
	BootstrapSecretName string `json:"bootstrapSecretName,omitempty"`

	// provisioningStartTime is the start of the WoL and metadata access window.
	// +optional
	ProvisioningStartTime *metav1.Time `json:"provisioningStartTime,omitempty"`

	// tokenExpiresAt is the deadline for serving bootstrap data.
	// +optional
	TokenExpiresAt *metav1.Time `json:"tokenExpiresAt,omitempty"`

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
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.ready`,description="Ready status"
// +kubebuilder:printcolumn:name="Host",type=string,JSONPath=`.status.hostRef.name`,description="Assigned TartHost"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`,description="CreationTimestamp is a timestamp representing the server time when this object was created."

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
