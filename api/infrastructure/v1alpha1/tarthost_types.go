package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	"github.com/walnuts1018/cluster-api-provider-tart/domain/endpoint"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
)

// TartHostのCondition typeを定義する。詳細はdocs/development/api-contract.mdを参照する。
const (
	TartHostReadyCondition          = "Ready"
	TartHostAvailableCondition      = "Available"
	TartHostInventoryReadyCondition = "InventoryReady"
	TartHostTalosReachableCondition = "TalosReachable"
)

// Tart関連Resourceで安全停止に共通利用する既知のReadyおよびAvailable reasonを定義する。
const (
	ReasonIdentityConflict = "IdentityConflict"
	// ReasonDiskIdentityConflictはIdentityConflictのうち、disk identity(WWIDまたはserial)の重複が原因であることを明示するreasonである。MAC addressやsystem UUIDの重複を含む他のIdentityConflictと区別してEventおよびlogから原因を追いやすくする。
	ReasonDiskIdentityConflict     = "DiskIdentityConflict"
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
	// ReasonRecoveryIdentityUnavailableはRetained Hostが保持する旧Talos installationのrecovery identityを解決できず、破壊的なReprovisionを開始できないことを示す。
	ReasonRecoveryIdentityUnavailable = "RecoveryIdentityUnavailable"
	// ReasonReprovisioningは承認されたReprovisionのTalos Resetを要求し、maintenance modeへの遷移を待っていることを示す。
	ReasonReprovisioning = "Reprovisioning"
)

// ReusePolicyはRetained TartHostを再利用できるかというユーザーの意図を表す。
// +kubebuilder:validation:Enum=Retain;AllowReuse
type ReusePolicy string

const (
	// ReusePolicyRetainはRetained Hostを自動allocationの対象外にする。既定値である。
	ReusePolicyRetain ReusePolicy = "Retain"
	// ReusePolicyAllowReuseは一致するReuseApprovalとReuseModeが存在する場合にRetained Hostを再利用可能にする。
	ReusePolicyAllowReuse ReusePolicy = "AllowReuse"
)

// ReuseModeは次のTartMachineが再利用Hostをclaimする方法を選択する。
// +kubebuilder:validation:Enum=Adopt;Reprovision
type ReuseMode string

const (
	// ReuseModeAdoptは既存のTalos installationとdataを保持する。identity、cluster ID、secret generation、ProviderID、roleおよびversionの互換性が一致しなければならない。
	ReuseModeAdopt ReuseMode = "Adopt"
	// ReuseModeReprovisionは最初にHostを明示的にclaimしてidentityを再確認し、data破棄をTalos resetおよびinstaller lifecycleへ委譲する。
	ReuseModeReprovision ReuseMode = "Reprovision"
)

// PowerBackendはTartHostが提供する電源機能を識別する。
// +kubebuilder:validation:Enum=Redfish;WakeOnLAN;Manual
type PowerBackend string

const (
	PowerBackendRedfish   PowerBackend = "Redfish"
	PowerBackendWakeOnLAN PowerBackend = "WakeOnLAN"
	PowerBackendManual    PowerBackend = "Manual"
)

// ManagementNamespaceSecretReferenceはSecretを名前だけで参照する。TartHostはcluster-scoped resourceであるため、この参照はユーザー指定namespaceではなく固定されたprovider管理namespaceで解決する。
type ManagementNamespaceSecretReference struct {
	Name string `json:"name"`
}

// RedfishPowerConfigはRedfish経由のout-of-band電源制御と停止確認を設定する。
type RedfishPowerConfig struct {
	// addressはRedfish service rootのbase URLである。
	// +kubebuilder:validation:Type=string
	Address endpoint.HTTPSURL `json:"address"`
	// systemIDはRedfish ComputerSystemのidentifierである。空にできるのはSystems collectionが単一memberの場合だけである。
	// +optional
	SystemID string `json:"systemID,omitempty"`
	// credentialSecretRefはusernameとpassword keyを持つSecretをprovider管理namespaceから参照する。
	CredentialSecretRef ManagementNamespaceSecretReference `json:"credentialSecretRef"`
	// caSecretRefはRedfish endpointのTLS certificateを検証するcustom CA certificate bundleをca.crt keyに持つSecretを任意に参照する。provider管理namespaceから解決し、省略時はinsecureSkipVerifyがtrueでない限りsystem trust bundleを使う。
	// +optional
	CASecretRef *ManagementNamespaceSecretReference `json:"caSecretRef,omitempty"`
	// insecureSkipVerifyはRedfish endpointのTLS certificate検証を無効化する。custom CA検証が実行できない場合だけ使用する。
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

// WakeOnLANPowerConfigはWake-on-LANによる電源投入を設定する。このbackendの停止確認はShutdown RPC受理後にauthenticated Talos APIが到達不能になることを観測するが、物理的な電源断の証明にはならない。
type WakeOnLANPowerConfig struct {
	// broadcastAddressはマジックパケット送信に使用するnetwork broadcast addressである。
	// +kubebuilder:validation:Type=string
	// +optional
	BroadcastAddress network.UDPAddress `json:"broadcastAddress,omitempty,omitzero"`
}

// PowerSpecはHostの電源機能を定義する。
// +kubebuilder:validation:XValidation:rule="(self.backend == 'Redfish' && has(self.redfish) && !has(self.wakeOnLAN)) || (self.backend == 'WakeOnLAN' && has(self.wakeOnLAN) && !has(self.redfish)) || (self.backend == 'Manual' && !has(self.redfish) && !has(self.wakeOnLAN))",message="backend must select exactly its matching power configuration"
type PowerSpec struct {
	// backendは使用する電源機能の実装を選択する。
	Backend PowerBackend `json:"backend"`
	// +optional
	Redfish *RedfishPowerConfig `json:"redfish,omitempty"`
	// +optional
	WakeOnLAN *WakeOnLANPowerConfig `json:"wakeOnLAN,omitempty"`
}

// PreviousConsumerRefはMachine削除後のHostの直前consumerを記録する。controllerが管理し、ユーザーが直接設定しない。
type PreviousConsumerRef struct {
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	UID       types.UID `json:"uid"`
	// clusterIDは直前consumerが所属していたTartCluster.spec.clusterIDである。
	ClusterID string `json:"clusterID"`
}

// ReuseApprovalはRetained Hostの再利用を明示的に承認するユーザー入力である。現在のPreviousConsumerRef.UIDと照合し、claim成功時には消費しない。次のMachine削除でPreviousConsumerRefが変わると自動的に無効になる。
type ReuseApproval struct {
	PreviousConsumerUID types.UID `json:"previousConsumerUID"`
}

// DeletionApprovalはClaimedまたはRetained Hostをinventoryから削除することを承認する。controllerは現在のbindingとretention recordのUIDが一致する場合だけ受理する。
type DeletionApproval struct {
	ConsumerUID         types.UID `json:"consumerUID,omitempty"`
	PreviousConsumerUID types.UID `json:"previousConsumerUID,omitempty"`
}

// TartHostSpecは物理または仮想Host inventory entryのdesired stateを定義する。
type TartHostSpec struct {
	// hostIDはmetadata.uidとは独立してHostを識別するimmutableなrandom UUIDである。management clusterのbackupから同じ物理Host identityとProviderIDを復元するために使い、controllerが所有するspec fieldである。
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$"
	// +kubebuilder:validation:XValidation:rule="self == oldSelf || (oldSelf == '' && self != '')",message="hostID may only be initialized once and is immutable afterwards"
	// +optional
	HostID string `json:"hostID,omitempty"`

	// macAddressは他のinventoryが未知の段階でobserved boot attemptをこのHostへbindするための主enrollment identityである。
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^([0-9a-fA-F]{2}[:-]){5}([0-9a-fA-F]{2})$"
	MACAddress network.MACAddress `json:"macAddress"`

	// talosAPIAddressはこのHostのTalos APIへ到達できる任意のaddressまたはDNS nameである。到達性のhintまたはoverrideに過ぎず、controllerはconfiguration apply前にobserved MAC addressを検証する。
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MaxLength=512
	// +optional
	TalosAPIAddress network.Endpoint `json:"talosAPIAddress,omitempty"`

	// architectureはHost selectorの条件である。
	// +optional
	Architecture string `json:"architecture,omitempty"`

	// failureDomainはMachine.spec.failureDomainと照合するHost selectorの条件である。
	// +optional
	FailureDomain string `json:"failureDomain,omitempty"`

	// powerはこのHostの電源制御と停止確認の機能を定義する。
	Power PowerSpec `json:"power"`

	// consumerRefはcontrollerが管理するexclusive allocation bindingである。atomic compare-and-swap(resourceVersionを確認するUpdateまたはJSON Patchのtest)で確立し、通常のユーザー編集fieldとして扱わない。
	// +optional
	ConsumerRef *corev1.ObjectReference `json:"consumerRef,omitempty"`

	// previousConsumerRefはcontrollerが管理する直前consumerのrecordである。Machine削除後も保持し、明示的なreuse approvalがあるまでこのHostを再度自動allocationしない。
	// +optional
	PreviousConsumerRef *PreviousConsumerRef `json:"previousConsumerRef,omitempty"`

	// reusePolicyはRetained Hostを再利用できるかというユーザーの意図である。
	// +kubebuilder:default=Retain
	// +optional
	ReusePolicy ReusePolicy `json:"reusePolicy,omitempty"`

	// reuseApprovalはこのHostを再利用可能にする前に現在のpreviousConsumerRef.uidと一致しなければならない。
	// +optional
	ReuseApproval *ReuseApproval `json:"reuseApproval,omitempty"`

	// reuseModeはHostの再利用承認後にAdoptまたはReprovisionを選択する。
	// +optional
	ReuseMode ReuseMode `json:"reuseMode,omitempty"`

	// deletionApprovalはClaimedまたはRetained Hostを削除する前に現在のHost stateと一致しなければならない。Hostをinventoryから除去することだけを承認し、power off、Talos reset、disk wipeは実行しない。
	// +optional
	DeletionApproval *DeletionApproval `json:"deletionApproval,omitempty"`
}

// DiskInventoryはHostのraw hardware inventoryから観測したdiskである。
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

	// stableSelectorは、このdiskを同じHost上の他のdiskと区別できる最も具体的なTalos CEL disk
	// selectorのpreviewである。install targetの選択が実際に生成するselectorと同じ規則
	// (WWID→serial→bus path→size/rotationalの順)から算出した観測結果であり、ユーザーが
	// TartBootstrapConfigのraw patchでstorage documentのdisk selectorを書く際に、WWID等の
	// 生値を読み取って比較する必要がないようにするための参考情報である。他のdiskと区別できない
	// 場合は空になる。
	// +optional
	StableSelector string `json:"stableSelector,omitempty"`
}

// NetworkInterfaceInventoryはHostで観測したnetwork interfaceである。
type NetworkInterfaceInventory struct {
	Name string `json:"name,omitempty"`
	// +kubebuilder:validation:Type=string
	MACAddress network.MACAddress `json:"macAddress,omitempty,omitzero"`
	LinkState  string             `json:"linkState,omitempty"`
	Driver     string             `json:"driver,omitempty"`
	BusPath    string             `json:"busPath,omitempty"`
	Addresses  []string           `json:"addresses,omitempty"`
}

// HostInventoryはmaintenance Talos discoveryで観測したhardware inventoryである。systemUUIDはidentity evidenceの一つとして扱い、BIOSでの欠落、全て0の値、DIY hardwareでの重複があり得るため単独では信頼しない。
type HostInventory struct {
	// bootIDはこのinventoryを取得したTalos kernel bootの識別子である。再起動をまたいだ古いmaintenance endpointの観測を現在のbootと混同しないために使う。
	// +optional
	BootID string `json:"bootID,omitempty"`
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$"
	SystemUUID        string                      `json:"systemUUID,omitempty,omitzero"`
	Architecture      string                      `json:"architecture,omitempty"`
	Disks             []DiskInventory             `json:"disks,omitempty"`
	NetworkInterfaces []NetworkInterfaceInventory `json:"networkInterfaces,omitempty"`
}

// BootAttemptはHost inventoryで観測したTalos kernel bootの履歴である。履歴はboundedであり、allocationのidentityは常に現在のMAC、system UUID、disk観測で検証する。
type BootAttempt struct {
	// bootIDはTalos kernel bootのstable identifierである。
	BootID string `json:"bootID"`
	// firstObservedAtはこのbootを最初に観測した時刻である。
	FirstObservedAt metav1.Time `json:"firstObservedAt"`
	// lastObservedAtはこのbootを最後に観測した時刻である。
	LastObservedAt metav1.Time `json:"lastObservedAt"`
	// systemUUIDはboot時に観測したsystem UUIDであり、空の場合がある。
	SystemUUID string `json:"systemUUID,omitempty"`
	// endpointはこのbootのinventoryへ接続したendpointであり、credentialや秘密情報を含まない。
	Endpoint string `json:"endpoint,omitempty"`
}

// TalosIdentityReferenceは、このHostが現在保持しているTalos installationが属するTalos cluster identityと、そのrecovery Secretへの参照を表す。
// recovery Secretはprovider管理namespace上のimmutable Secretであり、Talos API CAのsigning materialだけを保持する。Machine、TartBootstrapConfig、Bootstrap SecretのGCとは独立した寿命を持ち、少なくとも1台のTartHostがこのidentityを参照する間は削除されない。
type TalosIdentityReference struct {
	// clusterIDはこのHostへinstallされているTalos clusterのIDである。
	ClusterID string `json:"clusterID"`
	// recoverySecretRefは短命なTalos API client certificateを再発行できるrecovery Secretをprovider管理namespaceから参照する。
	RecoverySecretRef ManagementNamespaceSecretReference `json:"recoverySecretRef"`
	// boundAtはこのHostがこのTalos identityへbindされたことを最初に観測した時刻である。
	// +optional
	BoundAt metav1.Time `json:"boundAt,omitempty,omitzero"`
}

// TartHostStatusはTartHostのobserved stateを定義する。
type TartHostStatus struct {
	// currentTalosIdentityRefはこのHostが現在保持しているTalos installationのidentityである。
	// Machineの寿命ではなくHost上のinstallationの寿命に従い、Reprovisionのreset完了またはHostのforgetまで保持する。
	// +optional
	CurrentTalosIdentityRef *TalosIdentityReference `json:"currentTalosIdentityRef,omitempty"`

	// +optional
	Inventory *HostInventory `json:"inventory,omitempty"`

	// bootAttemptsは直近のboot identity観測をboundedに保持する。古いattemptを現在のHost identityとして再利用せず、現在のinventoryと照合するための観測履歴である。
	// +optional
	// +listType=map
	// +listMapKey=bootID
	// +kubebuilder:validation:MaxItems=16
	BootAttempts []BootAttempt `json:"bootAttempts,omitempty"`

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

// TartHostはtarthosts APIのschemaである。TartHostは単一のCAPI Machineより長く存続する、物理または仮想Hostのmanagement cluster全体にわたるcluster-scoped inventoryである。
type TartHost struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TartHostSpec   `json:"spec,omitempty"`
	Status TartHostStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TartHostListはTartHostのlistである。
type TartHostList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartHost `json:"items"`
}
