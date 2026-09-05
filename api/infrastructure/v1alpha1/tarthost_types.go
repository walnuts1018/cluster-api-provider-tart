package v1alpha1

import (
	"uuid"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	"github.com/walnuts1018/cluster-api-provider-tart/domain/cluster"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/endpoint"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
)

// TartHost Condition types. See docs/development/api-contract.md.
const (
	TartHostReadyCondition          = "Ready"
	TartHostAvailableCondition      = "Available"
	TartHostInventoryReadyCondition = "InventoryReady"
	TartHostTalosReachableCondition = "TalosReachable"
)

// Well-known Ready/Available Reasons used across Tart-facing Resources for safe stops.
const (
	ReasonIdentityConflict         = "IdentityConflict"
	ReasonShutdownUnconfirmed      = "ShutdownUnconfirmed"
	ReasonUnsafeUpdate             = "UnsafeUpdate"
	ReasonSecretBundleUnavailable  = "SecretBundleUnavailable"
	ReasonRolledBack               = "RolledBack"
	ReasonNoEligibleHost           = "NoEligibleHost"
	ReasonHostNotFound             = "HostNotFound"
	ReasonHostClaimConflict        = "HostClaimConflict"
	ReasonHostMismatch             = "HostMismatch"
	ReasonHostIDUnavailable        = "HostIDUnavailable"
	ReasonDeletionApprovalRequired = "DeletionApprovalRequired"
	ReasonNotImplemented           = "NotImplemented"
	ReasonClaimed                  = "Claimed"
	ReasonRetained                 = "Retained"
	ReasonReuseApprovalRequired    = "ReuseApprovalRequired"
	ReasonAvailable                = "Available"
)

// ReusePolicy is the user intent for whether a Retained TartHost may be reused.
// +kubebuilder:validation:Enum=Retain;AllowReuse
type ReusePolicy string

const (
	// ReusePolicyRetain keeps a Retained Host out of automatic allocation. This is the default.
	ReusePolicyRetain ReusePolicy = "Retain"
	// ReusePolicyAllowReuse allows a Retained Host to become eligible for reuse once a matching
	// ReuseApproval and ReuseMode are also present.
	ReusePolicyAllowReuse ReusePolicy = "AllowReuse"
)

// ReuseMode selects how a reused Host is claimed by the next TartMachine.
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

// ManagementNamespaceSecretReference references a Secret by name only. TartHost is a
// cluster-scoped resource, so this reference is always resolved against the fixed provider
// management namespace rather than a user-supplied one.
type ManagementNamespaceSecretReference struct {
	Name string `json:"name"`
}

// RedfishPowerConfig configures out-of-band power control and stop confirmation via Redfish.
type RedfishPowerConfig struct {
	// address is the Redfish service root base URL.
	// +kubebuilder:validation:Type=string
	Address endpoint.HTTPSURL `json:"address"`
	// systemID is the Redfish ComputerSystem identifier. If empty, the first system is used.
	// +optional
	SystemID string `json:"systemID,omitempty"`
	// credentialSecretRef references a Secret with "username" and "password" keys, resolved
	// from the provider management namespace.
	CredentialSecretRef ManagementNamespaceSecretReference `json:"credentialSecretRef"`
	// caSecretRef optionally references a Secret containing a custom CA certificate bundle
	// under the "ca.crt" key for verifying the Redfish endpoint TLS certificate, resolved from
	// the provider management namespace. When omitted, the system trust bundle is used unless
	// insecureSkipVerify is true.
	// +optional
	CASecretRef *ManagementNamespaceSecretReference `json:"caSecretRef,omitempty"`
	// insecureSkipVerify disables TLS certificate verification for the Redfish endpoint.
	// It should only be used when custom CA verification is not feasible.
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

// WakeOnLANPowerConfig configures Wake-on-LAN power-on. Stop confirmation for this backend
// relies on observing that the authenticated Talos API becomes unreachable after a Shutdown
// RPC is accepted, which is not proof of physical power-off.
type WakeOnLANPowerConfig struct {
	// broadcastAddress is the network broadcast address used to send the magic packet.
	// +kubebuilder:validation:Type=string
	// +optional
	BroadcastAddress network.UDPAddress `json:"broadcastAddress,omitempty,omitzero"`
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

// PreviousConsumerRef records the previous consumer of a Host after Machine deletion. It is
// controller-managed and is never set directly by users.
type PreviousConsumerRef struct {
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	UID       types.UID `json:"uid"`
	// clusterID is the TartCluster.spec.clusterID the previous consumer belonged to.
	ClusterID cluster.ClusterID `json:"clusterID"`
}

// ReuseApproval is an explicit, user-provided approval to reuse a Retained Host. It is
// matched against the current PreviousConsumerRef.UID and is not consumed on a successful claim;
// it becomes invalid automatically when PreviousConsumerRef changes on the next Machine deletion.
type ReuseApproval struct {
	PreviousConsumerUID types.UID `json:"previousConsumerUID"`
}

// DeletionApproval authorizes removing a Claimed or Retained Host from inventory.
// The controller accepts it only when the UIDs match the current binding and retention records.
type DeletionApproval struct {
	ConsumerUID         types.UID `json:"consumerUID,omitempty"`
	PreviousConsumerUID types.UID `json:"previousConsumerUID,omitempty"`
}

// TartHostSpec defines the desired state of a physical or virtual Host inventory entry.
type TartHostSpec struct {
	// hostID is an immutable random UUID that identifies this Host independently of
	// metadata.uid, so backups of the management cluster can recreate the same
	// physical Host identity and ProviderID. This is a controller-owned spec field.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$"
	// +kubebuilder:validation:XValidation:rule="self == oldSelf || (oldSelf == '' && self != '')",message="hostID may only be initialized once and is immutable afterwards"
	// +optional
	HostID hostdomain.HostID `json:"hostID,omitempty,omitzero"`

	// macAddress is the primary enrollment identity used to bind an observed boot
	// attempt to this Host before any other inventory is known.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^([0-9a-fA-F]{2}[:-]){5}([0-9a-fA-F]{2})$"
	MACAddress network.MACAddress `json:"macAddress"`

	// talosAPIAddress is an optional address or DNS name where the Talos API for this
	// Host can be reached. It is a reachability hint / override only; the controller
	// still verifies the observed MAC address before applying configuration.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MaxLength=512
	// +optional
	TalosAPIAddress network.Endpoint `json:"talosAPIAddress,omitempty"`

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

	// previousConsumerRef is the controller-managed record of the previous consumer, kept
	// after Machine deletion so this Host is not auto-allocated again until an explicit
	// reuse approval is given.
	// +optional
	PreviousConsumerRef *PreviousConsumerRef `json:"previousConsumerRef,omitempty"`

	// reusePolicy is the user intent for whether a Retained Host may be reused.
	// +kubebuilder:default=Retain
	// +optional
	ReusePolicy ReusePolicy `json:"reusePolicy,omitempty"`

	// reuseApproval must match the current previousConsumerRef.uid before this Host becomes eligible for reuse.
	// +optional
	ReuseApproval *ReuseApproval `json:"reuseApproval,omitempty"`

	// reuseMode selects Adopt or Reprovision once the Host is approved for reuse.
	// +optional
	ReuseMode ReuseMode `json:"reuseMode,omitempty"`

	// deletionApproval must match the current Host state before a Claimed or Retained
	// Host can be deleted. It authorizes removing the Host from inventory only; it
	// never triggers power off, Talos reset, or disk wipe.
	// +optional
	DeletionApproval *DeletionApproval `json:"deletionApproval,omitempty"`
}

// DiskInventory is an observed disk on the Host raw hardware inventory.
// Stable Talos CEL disk selectors are generated by CLI or helper tools rather than
// hardcoded into this inventory schema.
type DiskInventory struct {
	SizeBytes  int64    `json:"sizeBytes"`
	DevicePath string   `json:"devicePath,omitempty"`
	Model      string   `json:"model,omitempty"`
	Serial     string   `json:"serial,omitempty"`
	WWID       string   `json:"wwid,omitempty"`
	BusPath    string   `json:"busPath,omitempty"`
	Transport  string   `json:"transport,omitempty"`
	Rotational bool     `json:"rotational"`
	ReadOnly   bool     `json:"readOnly"`
	Symlinks   []string `json:"symlinks,omitempty"`
}

// NetworkInterfaceInventory is an observed network interface on the Host.
type NetworkInterfaceInventory struct {
	Name string `json:"name,omitempty"`
	// +kubebuilder:validation:Type=string
	MACAddress network.MACAddress `json:"macAddress,omitempty,omitzero"`
	LinkState  string             `json:"linkState,omitempty"`
	Driver     string             `json:"driver,omitempty"`
	BusPath    string             `json:"busPath,omitempty"`
	Addresses  []string           `json:"addresses,omitempty"`
}

// HostInventory is the hardware inventory observed via maintenance Talos discovery.
// Note that systemUUID is treated as one piece of identity evidence and is not solely
// relied upon due to potential BIOS omissions, all-zero values, or duplicates in DIY hardware.
type HostInventory struct {
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$"
	SystemUUID        uuid.UUID                   `json:"systemUUID,omitempty,omitzero"`
	Architecture      string                      `json:"architecture,omitempty"`
	Disks             []DiskInventory             `json:"disks,omitempty"`
	NetworkInterfaces []NetworkInterfaceInventory `json:"networkInterfaces,omitempty"`
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
// +kubebuilder:resource:scope=Cluster,categories=cluster-api,shortName=thost
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Available",type=string,JSONPath=".status.conditions[?(@.type=='Available')].status"

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
