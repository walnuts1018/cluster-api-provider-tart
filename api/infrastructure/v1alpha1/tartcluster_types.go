package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// TartClusterのCondition typeを定義する。
const (
	TartClusterReadyCondition = "Ready"
)

// DisruptionPolicyはnode-disruptive update中のdrain failureを処理する方法を定義する。
// +kubebuilder:validation:Enum=Block;AllowDowntime
type DisruptionPolicy string

const (
	// DisruptionPolicyBlockは理由を問わずdrainに失敗した場合にnode-disruptive updateを停止する。既定値である。
	DisruptionPolicyBlock DisruptionPolicy = "Block"
	// DisruptionPolicyAllowDowntimeはavailability、PDB、capacityだけが理由でdrainに失敗した場合に限りgraceful shutdownまたはrebootを許可する。data、identity、Host、etcd、quorumの安全性検査は緩和しない。
	DisruptionPolicyAllowDowntime DisruptionPolicy = "AllowDowntime"
)

// TartClusterSpecはTartClusterのdesired stateを定義する。control plane endpointについて、Tartはcontrol plane endpoint(VIPやload balancer)をprovisionしないため、ユーザー、ClusterClass、または周辺TopologyがCluster.spec.controlPlaneEndpointを通じて供給する。Tart reconcilerはendpointが利用可能になるまで待機する。
type TartClusterSpec struct {
	// clusterIDはCluster.metadata.uidとは独立してworkload clusterを識別するimmutableなrandom UUIDである。controllerが最初のnon-dry-run作成時に一度だけ生成し、同じobjectに対して再生成しない。同じ名前で再作成したclusterには新しいclusterIDを割り当て、古いsecret bundleやRetained Hostを再利用しない。controller所有のspec fieldである。
	// +kubebuilder:validation:Pattern="^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$"
	// +kubebuilder:validation:XValidation:rule="self == oldSelf || (oldSelf == '' && self != '')",message="clusterID may only be initialized once and is immutable afterwards"
	// +optional
	// +kubebuilder:validation:Type=string
	ClusterID string `json:"clusterID,omitempty"`

	// updatePolicyはnode-disruptive updateでavailabilityだけを理由とするdrain failureを緩和できるか制御する。data、identity、Host、etcd、quorumの安全性検査は緩和しない。
	// +optional
	UpdatePolicy TartUpdatePolicy `json:"updatePolicy,omitempty,omitzero"`

	// caRotationRequestedGenerationは利用者が要求するCA rotation後のactive secret bundle generationである。
	// status.activeSecretGeneration + 1と一致する場合だけ新しいrotationの対象として扱い、それ以外の値は無視する。
	// rotation完了後、この値をさらにactiveSecretGeneration + 2以上へ進めることで次のrotationを要求できる。
	// +optional
	// +kubebuilder:validation:Minimum=1
	CARotationRequestedGeneration *int32 `json:"caRotationRequestedGeneration,omitempty"`
}

// TartUpdatePolicyはnode-disruptive updateのavailability policyを定義する。
type TartUpdatePolicy struct {
	// disruptionPolicyはnode-disruptive update中のdrain failureを処理する方法を制御する。
	// +kubebuilder:default=Block
	// +optional
	DisruptionPolicy DisruptionPolicy `json:"disruptionPolicy,omitempty,omitzero"`
}

// TartClusterStatusはTartClusterのobserved stateを定義する。
type TartClusterStatus struct {
	// +optional
	Initialization TartClusterInitializationStatus `json:"initialization,omitempty,omitzero"`

	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=100
	FailureDomains []clusterv1.FailureDomain `json:"failureDomains,omitempty"`

	// activeSecretGenerationは現在activeなcluster secret bundleのgenerationであり、1から始まりCA rotationごとに単調増加する。clusterctl moveやbackup restoreではStatusが保持されない場合があるが、cluster namespace内の既存immutable bundle Secretを調べることでactive generationを決定論的に再構築できる。
	// +optional
	ActiveSecretGeneration int32 `json:"activeSecretGeneration,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// TartClusterInitializationStatusは初回provisioningの到達点を追跡する。
// +kubebuilder:validation:MinProperties=1
type TartClusterInitializationStatus struct {
	// +optional
	Provisioned *bool `json:"provisioned,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1alpha1"
// +kubebuilder:resource:categories=cluster-api,shortName=tcl
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"

// TartClusterはtartclusters APIのschemaである。
type TartCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TartClusterSpec   `json:"spec,omitempty"`
	Status TartClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TartClusterListはTartClusterのlistである。
type TartClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartCluster `json:"items"`
}
