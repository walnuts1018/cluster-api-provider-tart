package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TartBootstrapConfigのCondition typeを定義する。
const (
	TartBootstrapConfigReadyCondition = "Ready"
)

// ConfigurationUpdatePolicyは、稼働中NodeへTalos machine configurationの差分をどのように適用するかを表す。
// in-place updateとreboot-free updateは別概念であり、rebootを伴う場合でも同一Machine、同一Host、同一local storageのまま
// apply、reboot、health recoveryで完結するならin-place updateとして扱う。Machine replacementへfallbackすることはない。
// +kubebuilder:validation:Enum=Auto;Live;Reboot;InitialOnly
type ConfigurationUpdatePolicy string

const (
	// ConfigurationUpdatePolicyAutoは既定値であり、providerがreboot要否を判断する。
	// Talos 1.14時点ではreboot要否を信頼できる形で判定できないため、安全側に倒してRebootと同じ扱いになる。
	ConfigurationUpdatePolicyAuto ConfigurationUpdatePolicy = "Auto"
	// ConfigurationUpdatePolicyLiveは、running systemへreboot-freeでapplyしてよいとユーザーが明示するadvanced optionである。
	// 適用に失敗してもRebootへ自動fallbackせず、明示的な失敗として停止する。
	ConfigurationUpdatePolicyLive ConfigurationUpdatePolicy = "Live"
	// ConfigurationUpdatePolicyRebootは、configuration applyの後にproviderが安全にNode rebootをorchestrateする。
	ConfigurationUpdatePolicyReboot ConfigurationUpdatePolicy = "Reboot"
	// ConfigurationUpdatePolicyInitialOnlyは、初回provisioning後に変更してはいけないconfigurationを表す。
	// 差分が検出された場合はReprovisionRequiredとして安全停止する。
	ConfigurationUpdatePolicyInitialOnly ConfigurationUpdatePolicy = "InitialOnly"
)

// TartBootstrapConfigUpdatePolicyはmachine configuration updateの適用方針を保持する。
type TartBootstrapConfigUpdatePolicy struct {
	// configurationはeffective Talos machine configurationの差分をどう適用するかを指定する。既定値はAutoである。
	// +kubebuilder:default=Auto
	// +optional
	Configuration ConfigurationUpdatePolicy `json:"configuration,omitempty"`
}

// TartBootstrapConfigSpecはTartBootstrapConfigのdesired stateを定義する。ユーザー所有のraw Talos configuration patchは全てimmutableなSecret-backed inputから供給し、このSpecへinline保存しない。
// field classification: configPatchesSecretRefはmutableかつUpdate Extensionが所有するlifecycleである。通常のBootstrap reconcilerは初回Bootstrap Secretだけを生成し、稼働中Nodeへin-place変更を適用せず、live updateはUpdate Extensionが実行する。
type TartBootstrapConfigSpec struct {
	// configPatchesSecretRefはユーザー所有のraw Talos configuration patchを保持するimmutable Secretを任意に参照する。参照先Secretはimmutable: trueを設定しなければならない。参照先Secretのcontractは、patches keyがTalos multi-document YAMLまたはJSON patch、value keyがcomplete Talos machine configurationである。
	// +optional
	ConfigPatchesSecretRef *corev1.LocalObjectReference `json:"configPatchesSecretRef,omitempty"`

	// updatePolicyはconfigPatchesSecretRefの変更によって生じるeffective machine configuration差分の適用方針である。
	// +optional
	UpdatePolicy TartBootstrapConfigUpdatePolicy `json:"updatePolicy,omitempty,omitzero"`
}

// EffectiveConfigurationUpdatePolicyは、未設定時の既定値を解決したconfiguration update policyを返す。
func (s TartBootstrapConfigSpec) EffectiveConfigurationUpdatePolicy() ConfigurationUpdatePolicy {
	if s.UpdatePolicy.Configuration == "" {
		return ConfigurationUpdatePolicyAuto
	}
	return s.UpdatePolicy.Configuration
}

// TartBootstrapConfigStatusはTartBootstrapConfigのobserved stateを定義する。
type TartBootstrapConfigStatus struct {
	// +optional
	Initialization TartBootstrapConfigInitializationStatus `json:"initialization,omitempty,omitzero"`

	// dataSecretNameはCAPI contract(single value key、cluster name label、owner reference)を満たす生成済みBootstrap Secretの決定論的な名前である。
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +optional
	DataSecretName string `json:"dataSecretName,omitempty"`

	// configurationDigestはsecret-bearing valueをredactしたeffective Talos machine configurationのcanonical semantic representationに対するSHA-256である。観測値であり、update safetyの正本ではない。
	// +optional
	ConfigurationDigest string `json:"configurationDigest,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// TartBootstrapConfigInitializationStatusは初回Secret作成を追跡する。
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

// TartBootstrapConfigはtartbootstrapconfigs APIのschemaである。
type TartBootstrapConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TartBootstrapConfigSpec   `json:"spec,omitempty"`
	Status TartBootstrapConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TartBootstrapConfigListはTartBootstrapConfigのlistである。
type TartBootstrapConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TartBootstrapConfig `json:"items"`
}
