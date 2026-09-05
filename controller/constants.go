package controller

const (
	tartControlPlaneKind              = "TartControlPlane"
	tartClusterKind                   = "TartCluster"
	tartMachineKind                   = "TartMachine"
	tartBootstrapConfigKind           = "TartBootstrapConfig"
	capiMachineKind                   = "Machine"
	reasonClusterUnavailable          = "ClusterUnavailable"
	reasonBootstrapTemplateInvalid    = "BootstrapTemplateInvalid"
	reasonSecretBundleUnavailable     = "SecretBundleUnavailable"
	reasonMachineNameInvalid          = "MachineNameInvalid"
	reasonWorkloadAPIUnavailable      = "WorkloadAPIUnavailable"
	bootstrapConfigNameInvalidMessage = "A deterministic BootstrapConfig name is invalid."
)
