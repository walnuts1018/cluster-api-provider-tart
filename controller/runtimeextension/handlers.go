package runtimeextension

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
)

const (
	updateRetryAfterSeconds         int32 = 30
	talosUpdateTimeout                    = 20 * time.Second
	updateCapiMachineKind                 = "Machine"
	tartMachineKind                       = "TartMachine"
	tartBootstrapConfigKind               = "TartBootstrapConfig"
	tartBootstrapConfigTemplateKind       = "TartBootstrapConfigTemplate"
	imageField                            = "image"
	versionField                          = "version"
	unsafeUpdateMessage                   = "The requested in-place update contains an unsupported or unsafe difference; no patch was returned."
	updateClientUnavailable               = "The Runtime Extension Kubernetes client is unavailable; the update cannot be executed safely."
	updateVersionRejected                 = "The requested Talos version transition is not supported; the in-place update is stopped."
)

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tartmachines,verbs=get
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tarthosts,verbs=get
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=tartbootstrapconfigs,verbs=get
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

type jsonPatchOperation struct {
	Operation string `json:"op"`
	Path      string `json:"path"`
	Value     any    `json:"value"`
}

// canUpdateMachineはTartMachineのinstaller image変更だけをin-place updateとして認める。その他の差分は完全に評価できないためpatchなしでvetoする。
func canUpdateMachine(_ context.Context, req *runtimehooksv1.CanUpdateMachineRequest, resp *runtimehooksv1.CanUpdateMachineResponse) {
	resp.MachinePatch = runtimehooksv1.Patch{}
	resp.InfrastructureMachinePatch = runtimehooksv1.Patch{}
	resp.BootstrapConfigPatch = runtimehooksv1.Patch{}
	if req == nil {
		resp.Status = runtimehooksv1.ResponseStatusFailure
		resp.Message = unsafeUpdateMessage
		return
	}
	machinePatch, infrastructurePatch, bootstrapPatch, err := planMachineUpdate(req)
	if err != nil {
		resp.Status = runtimehooksv1.ResponseStatusFailure
		resp.Message = unsafeUpdateMessage
		return
	}
	resp.Status = runtimehooksv1.ResponseStatusSuccess
	resp.Message = "The requested Talos image update is covered by a complete in-place patch."
	resp.MachinePatch = machinePatch
	resp.InfrastructureMachinePatch = infrastructurePatch
	resp.BootstrapConfigPatch = bootstrapPatch
}

// canUpdateMachineSetはMachineSet templateのTartMachine image変更だけをin-place updateとして認める。
func canUpdateMachineSet(_ context.Context, req *runtimehooksv1.CanUpdateMachineSetRequest, resp *runtimehooksv1.CanUpdateMachineSetResponse) {
	resp.MachineSetPatch = runtimehooksv1.Patch{}
	resp.InfrastructureMachineTemplatePatch = runtimehooksv1.Patch{}
	resp.BootstrapConfigTemplatePatch = runtimehooksv1.Patch{}
	if req == nil {
		resp.Status = runtimehooksv1.ResponseStatusFailure
		resp.Message = unsafeUpdateMessage
		return
	}
	machinePatch, infrastructurePatch, bootstrapPatch, err := planMachineSetUpdate(req)
	if err != nil {
		resp.Status = runtimehooksv1.ResponseStatusFailure
		resp.Message = unsafeUpdateMessage
		return
	}
	resp.Status = runtimehooksv1.ResponseStatusSuccess
	resp.Message = "The requested Talos image update is covered by a complete in-place template patch."
	resp.MachineSetPatch = machinePatch
	resp.InfrastructureMachineTemplatePatch = infrastructurePatch
	resp.BootstrapConfigTemplatePatch = bootstrapPatch
}

func newUpdateMachineHandler(kubeClient client.Reader) func(context.Context, *runtimehooksv1.UpdateMachineRequest, *runtimehooksv1.UpdateMachineResponse) {
	return func(ctx context.Context, req *runtimehooksv1.UpdateMachineRequest, resp *runtimehooksv1.UpdateMachineResponse) {
		updateMachineWithClient(ctx, req, resp, kubeClient)
	}
}
