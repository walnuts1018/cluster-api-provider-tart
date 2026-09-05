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
type TartBootstrapConfigSpec struct {
	// configSecretRef optionally references an immutable Secret holding user-owned raw
	// Talos configuration patches. The referenced Secret must set immutable: true.
	// +optional
	ConfigSecretRef *corev1.LocalObjectReference `json:"configSecretRef,omitempty"`
}

// TartBootstrapConfigStatus defines the observed state of a TartBootstrapConfig.
type TartBootstrapConfigStatus struct {
	// +optional
	Initialization TartBootstrapConfigInitializationStatus `json:"initialization,omitempty"`

	// dataSecretName is the deterministically derived name of the generated Bootstrap
	// Secret.
	// +optional
	DataSecretName *string `json:"dataSecretName,omitempty"`

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
type TartBootstrapConfigInitializationStatus struct {
	// +optional
	DataSecretCreated *bool `json:"dataSecretCreated,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1alpha1"
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
