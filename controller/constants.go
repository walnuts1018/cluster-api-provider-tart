package controller

const (
	tartControlPlaneKind              = "TartControlPlane"
	tartMachineKind                   = "TartMachine"
	tartBootstrapConfigKind           = "TartBootstrapConfig"
	reasonClusterUnavailable          = "ClusterUnavailable"
	reasonBootstrapTemplateInvalid    = "BootstrapTemplateInvalid"
	reasonSecretBundleUnavailable     = "SecretBundleUnavailable"
	reasonMachineNameInvalid          = "MachineNameInvalid"
	bootstrapConfigNameInvalidMessage = "A deterministic BootstrapConfig name is invalid."
)
