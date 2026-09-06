package runtimeextension

import (
	"encoding/json/v2"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	"sigs.k8s.io/cluster-api/api/core/v1beta2"
	runtimehooksv1 "sigs.k8s.io/cluster-api/api/runtime/hooks/v1alpha1"
)

func TestMachineWasPreviouslyUpToDate(t *testing.T) {
	t.Parallel()

	upToDate := &infrav1alpha1.TartMachine{
		Status: infrav1alpha1.TartMachineStatus{Conditions: []metav1.Condition{{
			Type:   infrav1alpha1.TartMachineTalosUpToDateCondition,
			Status: metav1.ConditionTrue,
		}}},
	}
	if !machineWasPreviouslyUpToDate(upToDate) {
		t.Fatal("machineWasPreviouslyUpToDate() = false, want true")
	}
	upToDate.Status.Conditions[0].Status = metav1.ConditionFalse
	if machineWasPreviouslyUpToDate(upToDate) {
		t.Fatal("machineWasPreviouslyUpToDate() = true for a false condition")
	}
}

func TestCanUpdateMachineAllowsOnlyTalosImageChange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		desired    string
		bootstrap  runtime.RawExtension
		status     runtimehooksv1.ResponseStatus
		patch      bool
		wantReason string
	}{
		{
			name:    "image",
			desired: `{"image":{"version":"v1.14.0","schematicID":"new"},"hostSelector":{"architecture":"amd64"}}`,
			status:  runtimehooksv1.ResponseStatusSuccess,
			patch:   true,
		},
		{
			name:       "host selector",
			desired:    `{"image":{"version":"v1.14.0","schematicID":"old"},"hostSelector":{"architecture":"arm64"}}`,
			status:     runtimehooksv1.ResponseStatusFailure,
			wantReason: "unsupported host selector change",
		},
		{
			name:       "bootstrap configuration",
			desired:    `{"image":{"version":"v1.14.0","schematicID":"old"},"hostSelector":{"architecture":"amd64"}}`,
			bootstrap:  runtime.RawExtension{Raw: []byte(`{"spec":{"configPatchesSecretRef":{"name":"changed"}}}`)},
			status:     runtimehooksv1.ResponseStatusFailure,
			wantReason: "unsupported bootstrap configuration change",
		},
		{
			name:       "invalid image version",
			desired:    `{"image":{"version":"not-a-version","schematicID":"old"},"hostSelector":{"architecture":"amd64"}}`,
			status:     runtimehooksv1.ResponseStatusFailure,
			wantReason: "invalid Talos image version",
		},
	}

	current := runtime.RawExtension{Raw: []byte(`{"apiVersion":"infrastructure.cluster.x-k8s.io/v1alpha1","kind":"TartMachine","spec":{"image":{"version":"v1.13.0","schematicID":"old"},"hostSelector":{"architecture":"amd64"}}}`)}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := &runtimehooksv1.CanUpdateMachineRequest{
				Current: runtimehooksv1.CanUpdateMachineRequestObjects{
					Machine:               v1beta2.Machine{},
					InfrastructureMachine: current,
				},
				Desired: runtimehooksv1.CanUpdateMachineRequestObjects{
					Machine:               v1beta2.Machine{},
					InfrastructureMachine: runtime.RawExtension{Raw: []byte(`{"apiVersion":"infrastructure.cluster.x-k8s.io/v1alpha1","kind":"TartMachine","spec":` + test.desired + `}`)},
					BootstrapConfig:       test.bootstrap,
				},
			}
			response := &runtimehooksv1.CanUpdateMachineResponse{}
			canUpdateMachine(t.Context(), request, response)
			if response.Status != test.status {
				t.Fatalf("status = %q, want %q (%s)", response.Status, test.status, test.wantReason)
			}
			if response.InfrastructureMachinePatch.IsDefined() != test.patch {
				t.Fatalf("infrastructure patch defined = %t, want %t", response.InfrastructureMachinePatch.IsDefined(), test.patch)
			}
		})
	}
}

func TestCanUpdateMachineSetAllowsImageChangeAndRejectsTemplateMutation(t *testing.T) {
	t.Parallel()

	current := runtime.RawExtension{Raw: []byte(`{"apiVersion":"infrastructure.cluster.x-k8s.io/v1alpha1","kind":"TartMachineTemplate","spec":{"template":{"spec":{"image":{"version":"v1.13.0","schematicID":"old"},"hostSelector":{"architecture":"amd64"}}}}}`)}
	desired := runtime.RawExtension{Raw: []byte(`{"apiVersion":"infrastructure.cluster.x-k8s.io/v1alpha1","kind":"TartMachineTemplate","spec":{"template":{"spec":{"image":{"version":"v1.14.0","schematicID":"new"},"hostSelector":{"architecture":"amd64"}}}}}`)}
	request := &runtimehooksv1.CanUpdateMachineSetRequest{
		Current: runtimehooksv1.CanUpdateMachineSetRequestObjects{
			MachineSet:                    v1beta2.MachineSet{},
			InfrastructureMachineTemplate: current,
		},
		Desired: runtimehooksv1.CanUpdateMachineSetRequestObjects{
			MachineSet:                    v1beta2.MachineSet{},
			InfrastructureMachineTemplate: desired,
		},
	}
	response := &runtimehooksv1.CanUpdateMachineSetResponse{}
	canUpdateMachineSet(t.Context(), request, response)
	if response.Status != runtimehooksv1.ResponseStatusSuccess || !response.InfrastructureMachineTemplatePatch.IsDefined() {
		t.Fatalf("image update response = %#v, want success with patch", response)
	}

	desired = runtime.RawExtension{Raw: []byte(`{"apiVersion":"infrastructure.cluster.x-k8s.io/v1alpha1","kind":"TartMachineTemplate","spec":{"template":{"spec":{"image":{"version":"v1.14.0","schematicID":"new"},"hostSelector":{"architecture":"arm64"}}}}}`)}
	request.Desired.InfrastructureMachineTemplate = desired
	response = &runtimehooksv1.CanUpdateMachineSetResponse{}
	canUpdateMachineSet(t.Context(), request, response)
	if response.Status != runtimehooksv1.ResponseStatusFailure || response.InfrastructureMachineTemplatePatch.IsDefined() {
		t.Fatalf("template mutation response = %#v, want failure without patch", response)
	}
}

func TestCanUpdateMachineRejectsMalformedRequest(t *testing.T) {
	t.Parallel()

	response := &runtimehooksv1.CanUpdateMachineResponse{}
	canUpdateMachine(t.Context(), nil, response)
	if response.Status != runtimehooksv1.ResponseStatusFailure || response.InfrastructureMachinePatch.IsDefined() {
		t.Fatalf("malformed request response = %#v, want failure without patch", response)
	}
}

func TestUpdateMachineRejectsProviderOwnedByAnotherMachine(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add infrastructure scheme: %v", err)
	}
	controller := true
	providerMachine := &infrav1alpha1.TartMachine{
		Namespace: "ns",
		Name:      "machine-infra",
		UID:       types.UID("provider-uid"),
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: v1beta2.GroupVersion.String(),
			Kind:       "Machine",
			Name:       "other-machine",
			UID:        types.UID("other-machine-uid"),
			Controller: &controller,
		}},
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(providerMachine).Build()
	request := &runtimehooksv1.UpdateMachineRequest{
		Desired: runtimehooksv1.UpdateMachineRequestObjects{
			Machine: v1beta2.Machine{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "machine", UID: types.UID("machine-uid")},
				Spec:       v1beta2.MachineSpec{InfrastructureRef: v1beta2.ContractVersionedObjectReference{APIGroup: infrav1alpha1.GroupVersion.Group, Kind: "TartMachine", Name: "machine-infra"}},
			},
			InfrastructureMachine: runtime.RawExtension{Raw: []byte(`{"apiVersion":"infrastructure.cluster.x-k8s.io/v1alpha1","kind":"TartMachine","spec":{"image":{"version":"v1.14.0","schematicID":"schematic"}}}`)},
		},
	}
	response := &runtimehooksv1.UpdateMachineResponse{}
	updateMachineWithClient(t.Context(), request, response, reader)
	if response.Status != runtimehooksv1.ResponseStatusFailure {
		t.Fatalf("status = %q, want failure", response.Status)
	}
}

func TestCanUpdateMachineReturnsCompleteInfrastructureSpecPatch(t *testing.T) {
	t.Parallel()

	request := &runtimehooksv1.CanUpdateMachineRequest{
		Current: runtimehooksv1.CanUpdateMachineRequestObjects{
			Machine:               v1beta2.Machine{},
			InfrastructureMachine: runtime.RawExtension{Raw: []byte(`{"apiVersion":"infrastructure.cluster.x-k8s.io/v1alpha1","kind":"TartMachine","spec":{"image":{"version":"v1.13.0","schematicID":"old"},"hostSelector":{"architecture":"amd64","selector":{"matchLabels":{"pool":"a"}}}}}`)},
		},
		Desired: runtimehooksv1.CanUpdateMachineRequestObjects{
			Machine:               v1beta2.Machine{},
			InfrastructureMachine: runtime.RawExtension{Raw: []byte(`{"apiVersion":"infrastructure.cluster.x-k8s.io/v1alpha1","kind":"TartMachine","spec":{"image":{"version":"v1.14.0","schematicID":"new"},"hostSelector":{"architecture":"amd64","selector":{"matchLabels":{"pool":"a"}}}}}`)},
		},
	}
	response := &runtimehooksv1.CanUpdateMachineResponse{}
	canUpdateMachine(t.Context(), request, response)
	if response.Status != runtimehooksv1.ResponseStatusSuccess {
		t.Fatalf("image update response = %#v, want success", response)
	}

	var operations []jsonPatchOperation
	if err := json.Unmarshal(response.InfrastructureMachinePatch.Patch, &operations); err != nil {
		t.Fatalf("unmarshal infrastructure patch: %v", err)
	}
	if len(operations) != 1 || operations[0].Operation != "replace" || operations[0].Path != "/spec" {
		t.Fatalf("infrastructure patch operations = %#v, want one complete /spec replacement", operations)
	}
	value, ok := operations[0].Value.(map[string]any)
	if !ok || value["image"] == nil || value["hostSelector"] == nil {
		t.Fatalf("infrastructure patch value = %#v, want complete desired spec", operations[0].Value)
	}
}

func TestCanUpdateMachineRejectsCAPIIdentityChange(t *testing.T) {
	t.Parallel()

	request := &runtimehooksv1.CanUpdateMachineRequest{
		Current: runtimehooksv1.CanUpdateMachineRequestObjects{
			Machine:               v1beta2.Machine{Spec: v1beta2.MachineSpec{ClusterName: "cluster-a"}},
			InfrastructureMachine: runtime.RawExtension{Raw: []byte(`{"apiVersion":"infrastructure.cluster.x-k8s.io/v1alpha1","kind":"TartMachine","spec":{"image":{"version":"v1.13.0","schematicID":"old"}}}`)},
		},
		Desired: runtimehooksv1.CanUpdateMachineRequestObjects{
			Machine:               v1beta2.Machine{Spec: v1beta2.MachineSpec{ClusterName: "cluster-b"}},
			InfrastructureMachine: runtime.RawExtension{Raw: []byte(`{"apiVersion":"infrastructure.cluster.x-k8s.io/v1alpha1","kind":"TartMachine","spec":{"image":{"version":"v1.14.0","schematicID":"new"}}}`)},
		},
	}
	response := &runtimehooksv1.CanUpdateMachineResponse{}
	canUpdateMachine(t.Context(), request, response)
	if response.Status != runtimehooksv1.ResponseStatusFailure {
		t.Fatalf("identity change status = %q, want failure", response.Status)
	}
	if response.MachinePatch.IsDefined() || response.InfrastructureMachinePatch.IsDefined() {
		t.Fatalf("identity change returned patches: machine=%#v infrastructure=%#v", response.MachinePatch, response.InfrastructureMachinePatch)
	}
}

func TestCanUpdateMachineAllowsKubernetesVersionPropagation(t *testing.T) {
	t.Parallel()

	request := &runtimehooksv1.CanUpdateMachineRequest{
		Current: runtimehooksv1.CanUpdateMachineRequestObjects{
			Machine:               v1beta2.Machine{Spec: v1beta2.MachineSpec{Version: "v1.31.0"}},
			InfrastructureMachine: runtime.RawExtension{Raw: []byte(`{"apiVersion":"infrastructure.cluster.x-k8s.io/v1alpha1","kind":"TartMachine","spec":{"image":{"version":"v1.13.0","schematicID":"old"}}}`)},
		},
		Desired: runtimehooksv1.CanUpdateMachineRequestObjects{
			Machine:               v1beta2.Machine{Spec: v1beta2.MachineSpec{Version: "v1.32.0"}},
			InfrastructureMachine: runtime.RawExtension{Raw: []byte(`{"apiVersion":"infrastructure.cluster.x-k8s.io/v1alpha1","kind":"TartMachine","spec":{"image":{"version":"v1.14.0","schematicID":"new"}}}`)},
		},
	}
	response := &runtimehooksv1.CanUpdateMachineResponse{}
	canUpdateMachine(t.Context(), request, response)
	// Kubernetes version upgradeそのものはTartControlPlaneが所有するため、ここではdesired versionの伝播だけを許可する。
	if response.Status != runtimehooksv1.ResponseStatusSuccess {
		t.Fatalf("status = %q, want success", response.Status)
	}
	if !response.MachinePatch.IsDefined() {
		t.Fatalf("machine patch = %#v, want the desired Kubernetes version propagation", response.MachinePatch)
	}
}
