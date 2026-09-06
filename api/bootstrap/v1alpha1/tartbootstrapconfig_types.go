package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TartBootstrapConfigのCondition typeを定義する。
const (
	TartBootstrapConfigReadyCondition = "Ready"
)

// ConfigurationApplyStrategyは、稼働中NodeへTalos machine configurationを適用するproviderの方針を表す。
// RebootはSTAGED apply後にproviderが安全なrebootを起動する。NoRebootは通常のTalos applyだけを行い、即時完全反映やrebootを保証しない。
// +kubebuilder:validation:Enum=Reboot;NoReboot
type ConfigurationApplyStrategy string

const (
	// ConfigurationApplyStrategyRebootは、STAGED apply後にproviderが安全にNode rebootをorchestrateする。
	ConfigurationApplyStrategyReboot ConfigurationApplyStrategy = "Reboot"
	// ConfigurationApplyStrategyNoRebootは、Talosへ通常のconfiguration applyだけを行い、providerからrebootを起動しない。
	ConfigurationApplyStrategyNoReboot ConfigurationApplyStrategy = "NoReboot"
)

// TartBootstrapConfigUpdatePolicyはmachine configuration updateの適用方針を保持する。
type TartBootstrapConfigUpdatePolicy struct {
	// configurationはeffective Talos machine configurationの適用strategyを指定する。既定値はRebootである。
	// +kubebuilder:default=Reboot
	// +optional
	Configuration ConfigurationApplyStrategy `json:"configuration,omitempty"`
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

// EffectiveConfigurationApplyStrategyは、未設定時の既定値を解決したconfiguration apply strategyを返す。
func (s TartBootstrapConfigSpec) EffectiveConfigurationApplyStrategy() ConfigurationApplyStrategy {
	if s.UpdatePolicy.Configuration == "" {
		return ConfigurationApplyStrategyReboot
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
