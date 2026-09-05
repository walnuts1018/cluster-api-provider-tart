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
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// TartHost Condition types. See docs/development/api-contract.md.
const (
	TartHostReadyCondition          = "Ready"
	TartHostInventoryReadyCondition = "InventoryReady"
	TartHostTalosReachableCondition = "TalosReachable"
	TartHostClaimedCondition        = "Claimed"
	TartHostRetainedCondition       = "Retained"
	TartHostReusableCondition       = "Reusable"
)

// Well-known Ready/Available Reasons used across Tart-facing Resources for safe stops.
const (
	ReasonIdentityConflict        = "IdentityConflict"
	ReasonShutdownUnconfirmed     = "ShutdownUnconfirmed"
	ReasonUnsafeUpdate            = "UnsafeUpdate"
	ReasonSecretBundleUnavailable = "SecretBundleUnavailable"
	ReasonRolledBack              = "RolledBack"
	ReasonNotImplemented          = "NotImplemented"
)

// ReusePolicy is the user intent for whether a Retained TartHost may become Reusable.
// +kubebuilder:validation:Enum=Retain;Reusable
type ReusePolicy string

const (
	// ReusePolicyRetain keeps a Retained Host out of automatic allocation. This is the default.
	ReusePolicyRetain ReusePolicy = "Retain"
	// ReusePolicyReusable allows a Retained Host to become Reusable once a matching
	// ReuseApproval and ReuseMode are also present.
	ReusePolicyReusable ReusePolicy = "Reusable"
)

// ReuseMode selects how a Reusable Host is claimed by the next TartMachine.
// +kubebuilder:validation:Enum=Adopt;Reprovision
type ReuseMode string

const (
	// ReuseModeAdopt keeps existing Talos installation and data. It requires identity,
	// cluster ID, secret generation, ProviderID and role/version compatibility to match.
	ReuseModeAdopt ReuseMode = "Adopt"
	// ReuseModeReprovision explicitly claims the Host first, rechecks its identity,
	// and then delegates data destruction to the Talos reset/installer lifecycle.
	ReuseModeReprovision ReuseMode = "Reprovision"
)

// PowerBackend identifies which power capability a TartHost exposes.
// +kubebuilder:validation:Enum=Redfish;WakeOnLAN;Manual
type PowerBackend string

const (
	PowerBackendRedfish   PowerBackend = "Redfish"
	PowerBackendWakeOnLAN PowerBackend = "WakeOnLAN"
	PowerBackendManual    PowerBackend = "Manual"
)

// RedfishPowerConfig configures out-of-band power control and stop confirmation via Redfish.
type RedfishPowerConfig struct {
	// address is the Redfish service root base URL.
	Address string `json:"address"`
	// systemID is the Redfish ComputerSystem identifier. If empty, the first system is used.
	// +optional
	SystemID string `json:"systemID,omitempty"`
	// credentialSecretRef references a namespaced Secret with "username" and
	// "password" keys. The controller restricts the reference to the configured
	// management namespace.
	CredentialSecretRef corev1.SecretReference `json:"credentialSecretRef"`
	// insecureSkipVerify disables TLS certificate verification for the Redfish endpoint.
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

// WakeOnLANPowerConfig configures Wake-on-LAN power-on. Stop confirmation for this backend
// relies on observing that the authenticated Talos API becomes unreachable after a Shutdown
// RPC is accepted, which is not proof of physical power-off.
type WakeOnLANPowerConfig struct {
	// broadcastAddress is the network broadcast address used to send the magic packet.
	// +optional
	BroadcastAddress string `json:"broadcastAddress,omitempty"`
}

// PowerSpec describes the Host's power capability.
type PowerSpec struct {
	// backend selects which power capability implementation to use.
	// +kubebuilder:validation:XValidation:rule="(self.backend == 'Redfish' && has(self.redfish) && !has(self.wakeOnLAN)) || (self.backend == 'WakeOnLAN' && has(self.wakeOnLAN) && !has(self.redfish)) || (self.backend == 'Manual' && !has(self.redfish) && !has(self.wakeOnLAN))",message="backend must select exactly its matching power configuration"
	Backend PowerBackend `json:"backend"`
	// +optional
	Redfish *RedfishPowerConfig `json:"redfish,omitempty"`
	// +optional
	WakeOnLAN *WakeOnLANPowerConfig `json:"wakeOnLAN,omitempty"`
}

// RetainedFrom records the previous consumer of a Host after Machine deletion. It is
// controller-managed and is never set directly by users.
type RetainedFrom struct {
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	UID       types.UID `json:"uid"`
	// clusterID is the TartCluster.spec.id the previous consumer belonged to.
	ClusterID string `json:"clusterID"`
}

// ReuseApproval is an explicit, user-provided approval to reuse a Retained Host. It is
// matched against the current RetainedFrom.UID and is not consumed on a successful claim;
// it becomes invalid automatically when RetainedFrom changes on the next Machine deletion.
type ReuseApproval struct {
	RetainedFromUID types.UID `json:"retainedFromUID"`
}

// ForgetApproval authorizes forgetting one specific Host state. The controller
// accepts it only when both UIDs match the current binding and retained record.
type ForgetApproval struct {
	ConsumerUID     types.UID `json:"consumerUID,omitempty"`
	RetainedFromUID types.UID `json:"retainedFromUID,omitempty"`
}

// TartHostSpec defines the desired state of a physical or virtual Host inventory entry.
type TartHostSpec struct {
	// id is an immutable random UUID that identifies this Host independently of
	// metadata.uid, so backups of the management cluster can recreate the same
	// physical Host identity and ProviderID.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf || (oldSelf == '' && self != '')",message="id may only be initialized once and is immutable afterwards"
	// +optional
	ID string `json:"id,omitempty"`

	// macAddress is the primary enrollment identity used to bind an observed boot
	// attempt to this Host before any other inventory is known.
	MACAddress string `json:"macAddress"`

	// architecture is a Host selector criterion.
	// +optional
	Architecture string `json:"architecture,omitempty"`

	// failureDomain is a Host selector criterion matched against Machine.spec.failureDomain.
	// +optional
	FailureDomain string `json:"failureDomain,omitempty"`

	// power describes this Host's power control and stop-confirmation capability.
	Power PowerSpec `json:"power"`

	// consumerRef is the controller-managed exclusive allocation binding. It is
	// established via an atomic compare-and-swap (resourceVersion-checked Update or a
	// JSON Patch "test") and must not be treated as a normal user-editable field.
	// +optional
	ConsumerRef *corev1.ObjectReference `json:"consumerRef,omitempty"`

	// retainedFrom is the controller-managed record of the previous consumer, kept
	// after Machine deletion so this Host is not auto-allocated again until an explicit
	// reuse approval is given.
	// +optional
	RetainedFrom *RetainedFrom `json:"retainedFrom,omitempty"`

	// reusePolicy is the user intent for whether a Retained Host may become Reusable.
	// +kubebuilder:default=Retain
	// +optional
	ReusePolicy ReusePolicy `json:"reusePolicy,omitempty"`

	// reuseApproval must match the current retainedFrom.uid before this Host becomes Reusable.
	// +optional
	ReuseApproval *ReuseApproval `json:"reuseApproval,omitempty"`

	// reuseMode selects Adopt or Reprovision once the Host becomes Reusable.
	// +optional
	ReuseMode ReuseMode `json:"reuseMode,omitempty"`

	// forgetApproval must match the current Host state before a Claimed or Retained
	// Host can be deleted. It authorizes removing the Host from inventory only; it
	// never triggers power off, Talos reset, or disk wipe.
	// +optional
	ForgetApproval *ForgetApproval `json:"forgetApproval,omitempty"`
}

// DiskInventory is an observed disk on the Host, identified by a stable Talos disk
// selector rather than an unstable Linux device path.
type DiskInventory struct {
	Selector string `json:"selector"`
	SizeBytes  int64    `json:"sizeBytes"`
	DevPath    string   `json:"devPath,omitempty"`
	Model      string   `json:"model,omitempty"`
	Serial     string   `json:"serial,omitempty"`
	WWID       string   `json:"wwid,omitempty"`
	BusPath    string   `json:"busPath,omitempty"`
	Transport  string   `json:"transport,omitempty"`
	Rotational bool     `json:"rotational"`
	ReadOnly   bool     `json:"readonly"`
	Symlinks   []string `json:"symlinks,omitempty"`
}

// HostInventory is the hardware inventory observed via maintenance Talos discovery.
type HostInventory struct {
	SystemUUID string          `json:"systemUUID,omitempty"`
	Architecture string        `json:"architecture,omitempty"`
	Disks      []DiskInventory `json:"disks,omitempty"`
}

// TartHostStatus defines the observed state of a TartHost.
type TartHostStatus struct {
	// +optional
	Inventory *HostInventory `json:"inventory,omitempty"`

	// +optional
	Addresses clusterv1.MachineAddresses `json:"addresses,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Claimed",type=string,JSONPath=".status.conditions[?(@.type=='Claimed')].status"

// TartHost is the Schema for the tarthosts API. TartHost is a management-cluster-wide,
// cluster-scoped inventory of a physical or virtual Host that outlives any single CAPI
// Machine.
type TartHost struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TartHostSpec   `json:"spec,omitempty"`
	Status TartHostStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TartHostList contains a list of TartHost.
type TartHostList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartHost `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TartHost{}, &TartHostList{})
}
