package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// TartControlPlaneのCondition typeをdocs/development/api-contract.mdに従って定義する。CAPI v1beta2 ControlPlane contractに従い、AvailableをCluster.statusへbubble-upする主Conditionとする。
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
	// TartControlPlaneCARotatingConditionは、TartCluster.spec.caRotationRequestedGenerationで要求されたCA rotationの進行状況を示す。
	// Trueはgeneration N+1のPending bundleへ向けた段階的なCA切替が進行中であることを示し、program counterではなく毎回Talosとbundle Secretの観測から再計算する。
	TartControlPlaneCARotatingCondition = "CARotating"
)

// TartControlPlaneMachineTemplateDeletionSpecはnode-disruptiveな削除timeoutを保持する。rolloutを発生させないfieldとは分離する。
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

// TartControlPlaneMachineTemplateは各control-plane CAPI Machineを作成するtemplateである。
type TartControlPlaneMachineTemplate struct {
	// +optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// +optional
	Spec TartControlPlaneMachineTemplateSpec `json:"spec,omitempty,omitzero"`
}

// TartControlPlaneMachineTemplateSpecは各control-plane CAPI MachineのinfrastructureRef、削除動作、readiness gate、taintを作成するtemplateである。
type TartControlPlaneMachineTemplateSpec struct {
	// infrastructureRefはTartMachineTemplateを参照する。
	InfrastructureRef clusterv1.ContractVersionedObjectReference `json:"infrastructureRef"`

	// readinessGatesはMachine readinessを評価する追加Conditionを指定する。
	// +optional
	ReadinessGates []clusterv1.MachineReadinessGate `json:"readinessGates,omitempty,omitzero"`

	// taintsは作成したMachineへ適用するnode taintを指定する。
	// +optional
	Taints []clusterv1.MachineTaint `json:"taints,omitempty,omitzero"`

	// +optional
	Deletion TartControlPlaneMachineTemplateDeletionSpec `json:"deletion,omitempty,omitzero"`
}

// TartControlPlaneSpecはTartControlPlaneのdesired stateを定義する。Tartはcontrol plane endpoint(VIPやLB)をprovisionせず、endpointはCluster.spec.controlPlaneEndpointから外部に供給される。control planeのin-place updateはCAPI KCP patternに従い、CanUpdateMachine検証、Machine/InfraMachine/BootstrapConfigのdesired spec更新、in-place-updates.internal.cluster.x-k8s.io/update-in-progress annotation設定、UpdateMachine hook pending設定、Machine controller連携の順で扱う。
type TartControlPlaneSpec struct {
	// versionはdesired Kubernetes versionである。先頭にvを付けたsemantic versioningに従わなければならない。
	// +kubebuilder:validation:Pattern="^v[0-9]+\\.[0-9]+\\.[0-9]+.*$"
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	Version string `json:"version"`

	// replicasはdesired control plane Machine数である。
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	MachineTemplate TartControlPlaneMachineTemplate `json:"machineTemplate"`

	// bootstrapConfigTemplateRefは各control-plane MachineのTartBootstrapConfigをrenderするTartBootstrapConfigTemplateを参照する。
	BootstrapConfigTemplateRef clusterv1.ContractVersionedObjectReference `json:"bootstrapConfigTemplateRef"`
}

// TartControlPlaneStatusはTartControlPlaneのobserved stateを定義する。
type TartControlPlaneStatus struct {
	// +optional
	Initialization TartControlPlaneInitializationStatus `json:"initialization,omitempty,omitzero"`

	// versionsはcontrol-plane Machine群で観測した実際のKubernetes versionを古い順に並べる。top-levelのstatus.versionは持たない。
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

// TartControlPlaneInitializationStatusは初回etcd/API server bootstrapを追跡する。
// +kubebuilder:validation:MinProperties=1
type TartControlPlaneInitializationStatus struct {
	// controlPlaneInitializedはworkload Kubernetes API serverがrequestを受理した時点でtrueになる。全NodeがReadyであることやCNIがinstall済みであることは意味しない。
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

// TartControlPlaneはtartcontrolplanes APIのschemaである。
type TartControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TartControlPlaneSpec   `json:"spec,omitempty"`
	Status TartControlPlaneStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TartControlPlaneListはTartControlPlaneのlistである。
type TartControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartControlPlane `json:"items"`
}
