// Package controllerはCRDごとのReconcilerサブパッケージ(tarthost、tartmachineなど)から
// 共有される定数・helperを提供する。個別CRDだけが使う判定ロジックはサブパッケージ側に置く。
package controller

const (
	TartControlPlaneKind              = "TartControlPlane"
	TartClusterKind                   = "TartCluster"
	TartMachineKind                   = "TartMachine"
	TartBootstrapConfigKind           = "TartBootstrapConfig"
	CAPIMachineKind                   = "Machine"
	ReasonClusterUnavailable          = "ClusterUnavailable"
	ReasonBootstrapTemplateInvalid    = "BootstrapTemplateInvalid"
	ReasonSecretBundleUnavailable     = "SecretBundleUnavailable"
	ReasonMachineNameInvalid          = "MachineNameInvalid"
	ReasonWorkloadAPIUnavailable      = "WorkloadAPIUnavailable"
	ReasonMachineSpecMismatch         = "MachineSpecMismatch"
	ControlPlaneEtcdDeleteHookValue   = "true"
	BootstrapConfigNameInvalidMessage = "A deterministic BootstrapConfig name is invalid."
)
