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

package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type Architecture string

const (
	ArchitectureAMD64 Architecture = "amd64"
	ArchitectureARM64 Architecture = "arm64"
)

type Firmware string

const (
	FirmwareUEFI            Firmware = "UEFI"
	FirmwareLegacyBIOS      Firmware = "LegacyBIOS"
	FirmwareRaspberryPiBoot Firmware = "RaspberryPiEEPROM"
)

type PowerState string

const (
	PowerStateOn      PowerState = "On"
	PowerStateOff     PowerState = "Off"
	PowerStateUnknown PowerState = "Unknown"
)

type Capability string

const (
	CapabilityPowerOn           Capability = "PowerOn"
	CapabilityPowerOff          Capability = "PowerOff"
	CapabilityObservePowerState Capability = "ObservePowerState"
	CapabilitySetNextBoot       Capability = "SetNextBoot"
	CapabilityVirtualMedia      Capability = "VirtualMedia"
)

type TartHostSpec struct {
	// identifiers contains stable identifiers for the physical host.
	// +required
	Identifiers HostIdentifiers `json:"identifiers"`

	// architecture is the CPU architecture of the physical host.
	// +required
	// +kubebuilder:validation:Enum=amd64;arm64
	Architecture Architecture `json:"architecture"`

	// firmware is the firmware interface used to boot the host.
	// +required
	// +kubebuilder:validation:Enum=UEFI;LegacyBIOS;RaspberryPiEEPROM
	Firmware Firmware `json:"firmware"`

	// platformProfile identifies the versioned platform configuration required by this host.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	PlatformProfile string `json:"platformProfile"`

	// rootDeviceHints identifies the disk that may be modified by the Provisioning Agent.
	// +required
	RootDeviceHints RootDeviceHints `json:"rootDeviceHints"`

	// management selects the power and boot drivers used for this host.
	// +required
	Management HostManagement `json:"management"`

	// consumerRef identifies the TartMachine currently assigned to this host.
	// +optional
	ConsumerRef *ResourceReference `json:"consumerRef,omitempty"`
}

type HostIdentifiers struct {
	// systemUUID is the firmware-reported system UUID.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	SystemUUID string `json:"systemUUID,omitempty"`

	// bootMACAddress is the MAC address used for network boot.
	// +required
	// +kubebuilder:validation:Pattern=`^([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}$`
	BootMACAddress string `json:"bootMACAddress"`
}

type RootDeviceHints struct {
	// deviceName is a stable /dev/disk/by-id path.
	// +optional
	// +kubebuilder:validation:Pattern=`^/dev/disk/by-id/.+$`
	DeviceName string `json:"deviceName,omitempty"`

	// serialNumber is the disk serial number reported by the operating system.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	SerialNumber string `json:"serialNumber,omitempty"`

	// wwn is the disk World Wide Name.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	WWN string `json:"wwn,omitempty"`

	// minSizeBytes is the minimum acceptable disk capacity in bytes.
	// +required
	// +kubebuilder:validation:Minimum=1
	MinSizeBytes int64 `json:"minSizeBytes"`
}

type HostManagement struct {
	// powerDriver is the configured power driver name.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	PowerDriver string `json:"powerDriver"`

	// bootDriver is the configured boot driver name.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	BootDriver string `json:"bootDriver"`

	// credentialsSecretRef references credentials required by the configured drivers.
	// +optional
	CredentialsSecretRef *corev1.LocalObjectReference `json:"credentialsSecretRef,omitempty"`
}

type ResourceReference struct {
	// namespace is the namespace of the referenced resource.
	// +required
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`

	// name is the name of the referenced resource.
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// uid is the immutable UID of the referenced resource.
	// +required
	UID types.UID `json:"uid"`
}

type TartHostStatus struct {
	// phase is the current host lifecycle phase.
	// +optional
	// +kubebuilder:validation:Enum=Available;Reserved;Provisioning;Provisioned;Updating;Cleaning;Retained;Detached;RecoveryRequired;Error
	Phase TartHostPhase `json:"phase,omitempty"`

	// lastStablePhase is the stable phase to restore after an Error.
	// +optional
	// +kubebuilder:validation:Enum=Available;Provisioned;Retained;Detached
	LastStablePhase TartHostPhase `json:"lastStablePhase,omitempty"`

	// powerState is the last state reported by a PowerState observer.
	// +optional
	// +kubebuilder:validation:Enum=On;Off;Unknown
	PowerState PowerState `json:"powerState,omitempty"`

	// capabilities contains only operations implemented by the configured drivers.
	// +optional
	// +listType=set
	// +kubebuilder:validation:items:Enum=PowerOn;PowerOff;ObservePowerState;SetNextBoot;VirtualMedia
	Capabilities []Capability `json:"capabilities,omitempty"`

	// inventory contains the most recent hardware inventory reported by the Provisioning Agent.
	// +optional
	Inventory HostInventory `json:"inventory,omitempty,omitzero"`

	// conditions represent the current state of the TartHost.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the last spec generation reconciled into status.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

type TartHostPhase string

const (
	TartHostPhaseAvailable        TartHostPhase = "Available"
	TartHostPhaseReserved         TartHostPhase = "Reserved"
	TartHostPhaseProvisioning     TartHostPhase = "Provisioning"
	TartHostPhaseProvisioned      TartHostPhase = "Provisioned"
	TartHostPhaseUpdating         TartHostPhase = "Updating"
	TartHostPhaseCleaning         TartHostPhase = "Cleaning"
	TartHostPhaseRetained         TartHostPhase = "Retained"
	TartHostPhaseDetached         TartHostPhase = "Detached"
	TartHostPhaseRecoveryRequired TartHostPhase = "RecoveryRequired"
	TartHostPhaseError            TartHostPhase = "Error"
)

type HostInventory struct {
	// observedAt is the time at which the inventory was reported.
	// +optional
	ObservedAt *metav1.Time `json:"observedAt,omitempty"`

	// rootDisk contains the observed identity and size of the selected root disk.
	// +optional
	RootDisk ObservedDisk `json:"rootDisk,omitempty,omitzero"`
}

type ObservedDisk struct {
	// deviceName is the stable path reported for the disk.
	// +optional
	DeviceName string `json:"deviceName,omitempty"`

	// serialNumber is the reported disk serial number.
	// +optional
	SerialNumber string `json:"serialNumber,omitempty"`

	// wwn is the reported disk World Wide Name.
	// +optional
	WWN string `json:"wwn,omitempty"`

	// sizeBytes is the reported disk capacity in bytes.
	// +optional
	SizeBytes int64 `json:"sizeBytes,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=tarthosts,scope=Namespaced,categories=cluster-api
// +kubebuilder:unservedversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`,description="Host lifecycle phase"
// +kubebuilder:printcolumn:name="Architecture",type=string,JSONPath=`.spec.architecture`,description="CPU architecture"
// +kubebuilder:printcolumn:name="Consumer",type=string,JSONPath=`.spec.consumerRef.name`,description="Assigned TartMachine"

type TartHost struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              TartHostSpec   `json:"spec"`
	Status            TartHostStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

type TartHostList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartHost `json:"items"`
}

func init() {
	registerKnownTypes(&TartHost{}, &TartHostList{})
}
