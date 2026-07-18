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

package v1beta1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type OperationType string

const (
	OperationTypeProvision OperationType = "Provision"
	OperationTypeUpdate    OperationType = "Update"
	OperationTypeRollback  OperationType = "Rollback"
	OperationTypeClean     OperationType = "Clean"
	OperationTypeWipeAll   OperationType = "WipeAll"
	OperationTypeRecovery  OperationType = "Recovery"
)

type UpdateClass string

const (
	UpdateClassOSOnly           UpdateClass = "OSOnly"
	UpdateClassKubernetesBinary UpdateClass = "KubernetesBinary"
	UpdateClassStateMigration   UpdateClass = "StateMigration"
)

type TartHostOperationSpec struct {
	// operationID is the immutable UUID used to make external side effects idempotent.
	// +required
	// +kubebuilder:validation:Format=uuid
	OperationID string `json:"operationID"`

	// type identifies the workflow executed by this operation.
	// +required
	// +kubebuilder:validation:Enum=Provision;Update;Rollback;Clean;WipeAll;Recovery
	Type OperationType `json:"type"`

	// hostRef identifies the physical host locked by this operation.
	// +required
	HostRef ResourceReference `json:"hostRef"`

	// machineRef identifies the TartMachine associated with this operation.
	// It is omitted only for a manually requested WipeAll operation.
	// +optional
	MachineRef *ResourceReference `json:"machineRef,omitempty"`

	// planDigest is the digest of the canonical signed Plan.
	// +required
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	PlanDigest string `json:"planDigest"`

	// nodeLifecyclePlanDigest is the digest of the canonical signed Node Lifecycle Plan.
	// It is set only when distribution-specific lifecycle steps are required.
	// +optional
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	NodeLifecyclePlanDigest string `json:"nodeLifecyclePlanDigest,omitempty"`

	// desiredObjectsDigest identifies the canonical desired Kubernetes objects that produced the Plan.
	// +required
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	DesiredObjectsDigest string `json:"desiredObjectsDigest"`

	// targetImageDigest is required for update operations.
	// +optional
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	TargetImageDigest string `json:"targetImageDigest,omitempty"`

	// targetArtifactGeneration is required for update operations.
	// +optional
	// +kubebuilder:validation:Minimum=1
	TargetArtifactGeneration *int64 `json:"targetArtifactGeneration,omitempty"`

	// targetSlot is required for update operations.
	// +optional
	// +kubebuilder:validation:Enum=A;B
	TargetSlot OSSlot `json:"targetSlot,omitempty"`

	// updateClass is required for update operations.
	// +optional
	// +kubebuilder:validation:Enum=OSOnly;KubernetesBinary;StateMigration
	UpdateClass UpdateClass `json:"updateClass,omitempty"`

	// targetDistributionVersion is the Kubernetes distribution version requested by the Plan.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	TargetDistributionVersion string `json:"targetDistributionVersion,omitempty"`

	// deadline is the absolute time after which the operation must stop retrying.
	// +required
	Deadline metav1.Time `json:"deadline"`
}

type TartHostOperationStatus struct {
	// phase is the durable resume point of the operation.
	// +optional
	// +kubebuilder:validation:Enum=Pending;PreparingBoot;WaitingForAgent;Writing;Verifying;BootTrial;AwaitingHealth;DistributionUpdating;RollingBack;Succeeded;Failed;RecoveryRequired
	Phase TartHostOperationPhase `json:"phase,omitempty"`

	// lifecyclePhase identifies the current typed node lifecycle engine step.
	// +optional
	// +kubebuilder:validation:Enum=Preflight;Snapshot;Apply;Verify
	LifecyclePhase string `json:"lifecyclePhase,omitempty"`

	// snapshotRef identifies the snapshot required to recover a state migration.
	// +optional
	SnapshotRef *ResourceReference `json:"snapshotRef,omitempty"`

	// completedSteps is the set of successfully completed idempotent steps.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=128
	CompletedSteps []string `json:"completedSteps,omitempty"`

	// attempt is the number of attempts made for the current step.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Attempt int32 `json:"attempt,omitempty"`

	// agentSequence is the largest accepted monotonically increasing Agent report sequence.
	// +optional
	// +kubebuilder:validation:Minimum=0
	AgentSequence int64 `json:"agentSequence,omitempty"`

	// agentProgress is the latest accepted progress observation from the Agent.
	// +optional
	AgentProgress *AgentProgressStatus `json:"agentProgress,omitempty"`

	// sessionTokenHash is the SHA-256 hash of the active Session Token.
	// The plaintext token is never stored in Kubernetes.
	// +optional
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	SessionTokenHash string `json:"sessionTokenHash,omitempty"`

	// sessionTokenExpiresAt is the immutable expiry of the active Session Token.
	// +optional
	SessionTokenExpiresAt *metav1.Time `json:"sessionTokenExpiresAt,omitempty"`

	// sessionAuthenticationFailures is the number of rejected authentication attempts.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=5
	SessionAuthenticationFailures int32 `json:"sessionAuthenticationFailures,omitempty"`

	// sessionTokenConsumed indicates that Bootstrap delivery was claimed and may not be replayed.
	// +optional
	SessionTokenConsumed bool `json:"sessionTokenConsumed,omitempty"`

	// bootstrapDelivered indicates that Bootstrap Data was claimed once for this operation.
	// Issuing a new Session Token does not clear this operation-level replay guard.
	// +optional
	BootstrapDelivered bool `json:"bootstrapDelivered,omitempty"`

	// lastBootReport is the latest authenticated OS boot observation.
	// +optional
	LastBootReport *BootReportStatus `json:"lastBootReport,omitempty"`

	// conditions represent the current state of the TartHostOperation.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the last spec generation reconciled into status.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

type AgentProgressStatus struct {
	// step identifies the Plan step currently reported by the Agent.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	Step string `json:"step"`

	// diskRole identifies the payload target when the progress is disk-specific.
	// +optional
	// +kubebuilder:validation:Enum=Boot;OS-A;OS-B;Verity-A;Verity-B;State;Data
	DiskRole string `json:"diskRole,omitempty"`

	// percent is the completion percentage for the current step and disk role.
	// +required
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	Percent int32 `json:"percent"`
}

type BootReportStatus struct {
	// bootID identifies one boot of the installed operating system.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	BootID string `json:"bootID"`

	// machineID identifies the machine-id observed for this boot.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	MachineID string `json:"machineID"`

	// activeSlot is the slot observed by the operating system.
	// +required
	// +kubebuilder:validation:Enum=A;B
	ActiveSlot OSSlot `json:"activeSlot"`

	// artifactGeneration is the generation observed in the active slot.
	// +required
	// +kubebuilder:validation:Minimum=1
	ArtifactGeneration int64 `json:"artifactGeneration"`

	// stateMounted indicates that the required State filesystem is mounted.
	// +required
	StateMounted bool `json:"stateMounted"`

	// dataMounted indicates that the required Data filesystem is mounted.
	// +required
	DataMounted bool `json:"dataMounted"`

	// bootstrapApplied indicates that the Bootstrap success marker exists in State.
	// +required
	BootstrapApplied bool `json:"bootstrapApplied"`

	// bootstrapPayloadDigest is the payload digest recorded in the Bootstrap success marker.
	// It is present only when bootstrapApplied is true.
	// +optional
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	BootstrapPayloadDigest string `json:"bootstrapPayloadDigest,omitempty"`

	// reportedAt is the controller receipt time and is not supplied by the Agent.
	// +required
	ReportedAt metav1.Time `json:"reportedAt"`
}

type TartHostOperationPhase string

const (
	TartHostOperationPhasePending              TartHostOperationPhase = "Pending"
	TartHostOperationPhasePreparingBoot        TartHostOperationPhase = "PreparingBoot"
	TartHostOperationPhaseWaitingForAgent      TartHostOperationPhase = "WaitingForAgent"
	TartHostOperationPhaseWriting              TartHostOperationPhase = "Writing"
	TartHostOperationPhaseVerifying            TartHostOperationPhase = "Verifying"
	TartHostOperationPhaseBootTrial            TartHostOperationPhase = "BootTrial"
	TartHostOperationPhaseAwaitingHealth       TartHostOperationPhase = "AwaitingHealth"
	TartHostOperationPhaseDistributionUpdating TartHostOperationPhase = "DistributionUpdating"
	TartHostOperationPhaseRollingBack          TartHostOperationPhase = "RollingBack"
	TartHostOperationPhaseSucceeded            TartHostOperationPhase = "Succeeded"
	TartHostOperationPhaseFailed               TartHostOperationPhase = "Failed"
	TartHostOperationPhaseRecoveryRequired     TartHostOperationPhase = "RecoveryRequired"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=tarthostoperations,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`,description="Operation type"
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`,description="Operation phase"
// +kubebuilder:printcolumn:name="Host",type=string,JSONPath=`.spec.hostRef.name`,description="Target TartHost"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`

type TartHostOperation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              TartHostOperationSpec   `json:"spec"`
	Status            TartHostOperationStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

type TartHostOperationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartHostOperation `json:"items"`
}

func init() {
	registerKnownTypes(&TartHostOperation{}, &TartHostOperationList{})
}
