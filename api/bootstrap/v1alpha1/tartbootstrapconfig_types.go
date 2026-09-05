package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TartBootstrapConfig Condition types.
const (
	TartBootstrapConfigReadyCondition = "Ready"
)

// TartBootstrapConfigSpec defines the desired state of a TartBootstrapConfig. All
// user-owned raw Talos configuration patches are supplied via an immutable
// Secret-backed input; no raw patch is ever stored inline in this Spec.
//
// Field classification:
//   - configPatchesSecretRef: mutable / Update Extension-owned lifecycle.
//     The normal Bootstrap reconciler generates the initial Bootstrap Secret only and
//     does NOT apply in-place changes to running Nodes; live updates are driven by Update Extension.
type TartBootstrapConfigSpec struct {
	// configPatchesSecretRef optionally references an immutable Secret holding user-owned raw
	// Talos configuration patches. The referenced Secret must set immutable: true.
	// Contract for referenced Secret:
	// - "patches": Talos multi-document YAML or JSON patches
	// - "value": complete Talos machine configuration
	// +optional
	ConfigPatchesSecretRef *corev1.LocalObjectReference `json:"configPatchesSecretRef,omitempty"`
}

// TartBootstrapConfigStatus defines the observed state of a TartBootstrapConfig.
type TartBootstrapConfigStatus struct {
	// +optional
	Initialization TartBootstrapConfigInitializationStatus `json:"initialization,omitempty,omitzero"`

	// dataSecretName is the deterministically derived name of the generated Bootstrap
	// Secret satisfying the CAPI contract (single "value" key, cluster name label, owner reference).
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +optional
	DataSecretName string `json:"dataSecretName,omitempty"`

	// configurationDigest is the SHA-256 of the canonical semantic representation of
	// the effective Talos machine configuration, with secret-bearing values redacted.
	// It is an observation only and is not the source of truth for update safety.
	// +optional
	ConfigurationDigest string `json:"configurationDigest,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// TartBootstrapConfigInitializationStatus tracks initial Secret creation.
// +kubebuilder:validation:MinProperties=1
type TartBootstrapConfigInitializationStatus struct {
	// +optional
	DataSecretCreated *bool `json:"dataSecretCreated,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1alpha1"
// +kubebuilder:resource:categories=cluster-api,shortName=tbcfg
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="DataSecretName",type=string,JSONPath=".status.dataSecretName"

// TartBootstrapConfig is the Schema for the tartbootstrapconfigs API.
type TartBootstrapConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TartBootstrapConfigSpec   `json:"spec,omitempty"`
	Status TartBootstrapConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TartBootstrapConfigList contains a list of TartBootstrapConfig.
type TartBootstrapConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartBootstrapConfig `json:"items"`
}
