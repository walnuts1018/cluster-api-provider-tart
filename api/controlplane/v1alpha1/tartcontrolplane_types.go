package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// TartControlPlane Condition types, per docs/development/api-contract.md.
// In accordance with CAPI v1beta2 ControlPlane contract, Available is the primary condition
// bubble-up to Cluster.status.
const (
	TartControlPlaneAvailableCondition            = "Available"
	TartControlPlaneUpToDateCondition             = "UpToDate"
	TartControlPlaneRollingOutCondition           = "RollingOut"
	TartControlPlaneScalingUpCondition            = "ScalingUp"
	TartControlPlaneScalingDownCondition          = "ScalingDown"
	TartControlPlaneMachinesReadyCondition        = "MachinesReady"
	TartControlPlaneMachinesUpToDateCondition     = "MachinesUpToDate"
	TartControlPlaneEtcdClusterAvailableCondition = "EtcdClusterAvailable"
	TartControlPlaneDeletingCondition             = "Deleting"
	TartControlPlanePausedCondition               = "Paused"
)

// TartControlPlaneMachineTemplateDeletionSpec carries node-disruptive deletion
// timeouts, kept separate from fields that must not trigger a rollout.
// +kubebuilder:validation:MinProperties=1
type TartControlPlaneMachineTemplateDeletionSpec struct {
	// +optional
	// +kubebuilder:validation:Minimum=0
	NodeDrainTimeoutSeconds *int32 `json:"nodeDrainTimeoutSeconds,omitempty"`
	// +optional
	// +kubebuilder:validation:Minimum=0
	NodeVolumeDetachTimeoutSeconds *int32 `json:"nodeVolumeDetachTimeoutSeconds,omitempty"`
	// +optional
	// +kubebuilder:validation:Minimum=0
	NodeDeletionTimeoutSeconds *int32 `json:"nodeDeletionTimeoutSeconds,omitempty"`
}

// TartControlPlaneMachineTemplate is the template used to create each
// control-plane CAPI Machine.
type TartControlPlaneMachineTemplate struct {
	// +optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// +optional
	Spec TartControlPlaneMachineTemplateSpec `json:"spec,omitempty,omitzero"`
}

// TartControlPlaneMachineTemplateSpec is the template used to create each
// control-plane CAPI Machine's infrastructureRef, deletion behavior, readiness gates, and taints.
type TartControlPlaneMachineTemplateSpec struct {
	// infrastructureRef refers to a TartMachineTemplate.
	InfrastructureRef clusterv1.ContractVersionedObjectReference `json:"infrastructureRef"`

	// readinessGates specifies additional conditions to evaluate for Machine readiness.
	// +optional
	ReadinessGates []clusterv1.MachineReadinessGate `json:"readinessGates,omitempty,omitzero"`

	// taints specifies node taints applied to the created Machine.
	// +optional
	Taints []clusterv1.MachineTaint `json:"taints,omitempty,omitzero"`

	// +optional
	Deletion TartControlPlaneMachineTemplateDeletionSpec `json:"deletion,omitempty,omitzero"`
}

// TartControlPlaneSpec defines the desired state of a TartControlPlane.
//
// Control Plane endpoint note: Tart does not provision control plane endpoints (VIP, LB).
// The endpoint is expected to be provided externally via Cluster.spec.controlPlaneEndpoint.
//
// In-place update note: Control plane in-place updates follow the CAPI KCP pattern:
// CanUpdateMachine verification -> update Machine/InfraMachine/BootstrapConfig desired specs ->
// in-place-updates.internal.cluster.x-k8s.io/update-in-progress annotation -> UpdateMachine hook pending ->
// Machine controller coordination.
type TartControlPlaneSpec struct {
	// version is the desired Kubernetes version. It must follow semantic versioning with a leading "v".
	// +kubebuilder:validation:Pattern="^v[0-9]+\\.[0-9]+\\.[0-9]+.*$"
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Version string `json:"version"`

	// replicas is the desired number of control plane Machines.
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	MachineTemplate TartControlPlaneMachineTemplate `json:"machineTemplate"`

	// bootstrapConfigTemplateRef refers to a TartBootstrapConfigTemplate used to render
	// each control-plane Machine's TartBootstrapConfig.
	BootstrapConfigTemplateRef clusterv1.ContractVersionedObjectReference `json:"bootstrapConfigTemplateRef"`
}

// TartControlPlaneStatus defines the observed state of a TartControlPlane.
type TartControlPlaneStatus struct {
	// +optional
	Initialization TartControlPlaneInitializationStatus `json:"initialization,omitempty,omitzero"`

	// versions lists observed actual Kubernetes versions across control-plane Machines,
	// oldest to newest. There is no separate top-level status.version.
	// +optional
	// +listType=map
	// +listMapKey=version
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=100
	Versions []clusterv1.StatusVersion `json:"versions,omitempty"`

	// +optional
	Selector string `json:"selector,omitempty"`
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`
	// +optional
	ReadyReplicas *int32 `json:"readyReplicas,omitempty"`
	// +optional
	AvailableReplicas *int32 `json:"availableReplicas,omitempty"`
	// +optional
	UpToDateReplicas *int32 `json:"upToDateReplicas,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// TartControlPlaneInitializationStatus tracks initial etcd/API server bootstrap.
// +kubebuilder:validation:MinProperties=1
type TartControlPlaneInitializationStatus struct {
	// controlPlaneInitialized becomes true once the workload Kubernetes API server
	// accepts requests. It does not imply all Nodes are Ready or that CNI is installed.
	// +optional
	ControlPlaneInitialized *bool `json:"controlPlaneInitialized,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1alpha1"
// +kubebuilder:resource:categories=cluster-api,shortName=tcp
// +kubebuilder:printcolumn:name="Available",type=string,JSONPath=".status.conditions[?(@.type=='Available')].status"
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=".status.replicas"

// TartControlPlane is the Schema for the tartcontrolplanes API.
type TartControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TartControlPlaneSpec   `json:"spec,omitempty"`
	Status TartControlPlaneStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TartControlPlaneList contains a list of TartControlPlane.
type TartControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartControlPlane `json:"items"`
}
