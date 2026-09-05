package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// TartCluster Condition types.
const (
	TartClusterReadyCondition = "Ready"
)

// TartClusterSpec defines the desired state of a TartCluster.
type TartClusterSpec struct {
	// id is an immutable random UUID that identifies this workload cluster independently
	// of Cluster.metadata.uid. It is generated once by the controller on first
	// non-dry-run creation and is never regenerated for the same object; a cluster
	// recreated under the same name gets a new id and must not reuse old secret bundles
	// or Retained Hosts.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf || (oldSelf == '' && self != '')",message="id may only be initialized once and is immutable afterwards"
	// +optional
	ID string `json:"id,omitempty"`

	// updatePolicy controls whether availability-only drain failures may be
	// relaxed for node-disruptive updates. It never relaxes data, identity,
	// Host, etcd, or quorum safety checks.
	// +optional
	UpdatePolicy TartUpdatePolicy `json:"updatePolicy,omitempty,omitzero"`
}

// TartUpdatePolicy defines the availability policy for node-disruptive updates.
type TartUpdatePolicy struct {
	// allowDowntime allows graceful shutdown or reboot when drain fails only for
	// availability, PDB, or capacity reasons.
	// +optional
	AllowDowntime bool `json:"allowDowntime,omitempty,omitzero"`
}

// TartClusterStatus defines the observed state of a TartCluster.
type TartClusterStatus struct {
	// +optional
	Initialization TartClusterInitializationStatus `json:"initialization,omitempty"`

	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=100
	FailureDomains []clusterv1.FailureDomain `json:"failureDomains,omitempty"`

	// activeSecretGeneration is the currently active cluster secret bundle generation,
	// starting at 1 and monotonically increasing on CA rotation.
	// +optional
	ActiveSecretGeneration int32 `json:"activeSecretGeneration,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// TartClusterInitializationStatus tracks initial provisioning milestones.
type TartClusterInitializationStatus struct {
	// +optional
	Provisioned *bool `json:"provisioned,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1alpha1"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"

// TartCluster is the Schema for the tartclusters API.
type TartCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TartClusterSpec   `json:"spec,omitempty"`
	Status TartClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TartClusterList contains a list of TartCluster.
type TartClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartCluster `json:"items"`
}
