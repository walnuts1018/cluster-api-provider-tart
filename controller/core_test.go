package controller

import (
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

func TestSetConditionReplacesExistingCondition(t *testing.T) {
	t.Parallel()

	conditions := []metav1.Condition{{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "OldReason",
		Message:            "old message",
		ObservedGeneration: 2,
	}}

	SetCondition(&conditions, "Ready", metav1.ConditionTrue, "Ready", "the resource is ready", 3)

	if len(conditions) != 1 {
		t.Fatalf("SetCondition() created duplicate conditions: %#v", conditions)
	}
	got := conditions[0]
	if got.Status != metav1.ConditionTrue || got.Reason != "Ready" || got.Message != "the resource is ready" || got.ObservedGeneration != 3 {
		t.Errorf("SetCondition() = %#v, want updated condition", got)
	}
}

func TestSetPausedCondition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		paused bool
		status metav1.ConditionStatus
		reason string
	}{
		{name: "paused", paused: true, status: metav1.ConditionTrue, reason: "Paused"},
		{name: "not paused", paused: false, status: metav1.ConditionFalse, reason: "NotPaused"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var conditions []metav1.Condition
			SetPausedCondition(&conditions, tt.paused, 7)
			if len(conditions) != 1 {
				t.Fatalf("SetPausedCondition() conditions = %#v", conditions)
			}
			condition := conditions[0]
			if condition.Type != "Paused" || condition.Status != tt.status || condition.Reason != tt.reason || condition.ObservedGeneration != 7 {
				t.Errorf("SetPausedCondition() = %#v", condition)
			}
		})
	}
}

func TestHostTalosEndpointPrefersSpecAndKnownAddressTypes(t *testing.T) {
	t.Parallel()

	host := &infrav1alpha1.TartHost{
		Spec: infrav1alpha1.TartHostSpec{TalosAPIAddress: "192.0.2.10:50000"},
		Status: infrav1alpha1.TartHostStatus{Addresses: clusterv1.MachineAddresses{
			{Type: clusterv1.MachineExternalIP, Address: "192.0.2.11"},
			{Type: clusterv1.MachineInternalIP, Address: "192.0.2.12"},
		}},
	}
	if got := HostTalosEndpoint(host); got != "192.0.2.10:50000" {
		t.Errorf("HostTalosEndpoint() explicit = %q", got)
	}
	host.Spec.TalosAPIAddress = ""
	if got := HostTalosEndpoint(host); got != "192.0.2.12" {
		t.Errorf("HostTalosEndpoint() observed = %q, want internal address", got)
	}
	host.Status.Addresses = clusterv1.MachineAddresses{{Type: clusterv1.MachineHostName, Address: "host.test.walnuts.dev"}}
	if got := HostTalosEndpoint(host); got != "host.test.walnuts.dev" {
		t.Errorf("HostTalosEndpoint() hostname = %q", got)
	}
	host.Status.Addresses = nil
	if got := HostTalosEndpoint(host); got != "" {
		t.Errorf("HostTalosEndpoint() without endpoint = %q, want empty", got)
	}
}

func TestHostAddresses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		endpoint string
		wantType clusterv1.MachineAddressType
		wantAddr string
	}{
		{name: "ipv4 with port", endpoint: "192.0.2.10:50000", wantType: clusterv1.MachineInternalIP, wantAddr: "192.0.2.10"},
		{name: "ipv6 with port", endpoint: "[2001:db8::10]:50000", wantType: clusterv1.MachineInternalIP, wantAddr: "2001:db8::10"},
		{name: "hostname", endpoint: "host.test.walnuts.dev", wantType: clusterv1.MachineHostName, wantAddr: "host.test.walnuts.dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := HostAddresses(tt.endpoint)
			if len(got) != 1 || got[0].Type != tt.wantType || got[0].Address != tt.wantAddr {
				t.Errorf("HostAddresses(%q) = %#v", tt.endpoint, got)
			}
		})
	}
}

func TestValidateProviderOwner(t *testing.T) {
	t.Parallel()

	machine := &clusterv1.Machine{Name: "machine-a", UID: types.UID("machine-a")}
	validOwner := metav1.OwnerReference{APIVersion: clusterv1.GroupVersion.String(), Kind: CAPIMachineKind, Name: machine.Name, UID: machine.UID, Controller: new(true)}
	provider := &infrav1alpha1.TartMachine{OwnerReferences: []metav1.OwnerReference{validOwner}}
	if err := ValidateProviderOwner(provider, machine, clusterv1.GroupVersion.String(), CAPIMachineKind); err != nil {
		t.Fatalf("ValidateProviderOwner(valid) error = %v", err)
	}
	provider.OwnerReferences[0].Controller = new(false)
	var failure *OwnershipFailure
	if err := ValidateProviderOwner(provider, machine, clusterv1.GroupVersion.String(), CAPIMachineKind); !errors.As(err, &failure) {
		t.Fatalf("ValidateProviderOwner(invalid) error = %v, want OwnershipFailure", err)
	}
	if failure.Reason != "MachineOwnershipMismatch" {
		t.Errorf("OwnershipFailure.Reason = %q", failure.Reason)
	}
}

func TestFindCAPIMachineForInfrastructure(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(infrastructure) error = %v", err)
	}
	if err := clusterv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(cluster-api) error = %v", err)
	}
	if err := bootstrapv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(bootstrap) error = %v", err)
	}

	provider := &infrav1alpha1.TartMachine{Namespace: "ns", Name: "machine-a"}
	newMachine := func(name, uid string) *clusterv1.Machine {
		return &clusterv1.Machine{
			Namespace: "ns", Name: name, UID: types.UID(uid),
			Spec: clusterv1.MachineSpec{InfrastructureRef: clusterv1.ContractVersionedObjectReference{APIGroup: infrav1alpha1.GroupVersion.Group, Kind: TartMachineKind, Name: provider.Name}},
		}
	}

	tests := []struct {
		name     string
		machines []*clusterv1.Machine
		owner    *metav1.OwnerReference
		want     string
		wantErr  error
	}{
		{name: "finds single matching machine", machines: []*clusterv1.Machine{newMachine("machine-a", "uid-a")}, want: "machine-a"},
		{name: "rejects ambiguous matches", machines: []*clusterv1.Machine{newMachine("machine-a", "uid-a"), newMachine("machine-b", "uid-b")}, wantErr: ErrCAPIMachineAmbiguous},
		{name: "rejects owner UID drift", machines: []*clusterv1.Machine{newMachine("machine-a", "uid-current")}, owner: &metav1.OwnerReference{APIVersion: clusterv1.GroupVersion.String(), Kind: CAPIMachineKind, Name: "machine-a", UID: types.UID("uid-old")}, wantErr: ErrCAPIMachineIdentityMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			object := provider.DeepCopy()
			if tt.owner != nil {
				object.OwnerReferences = []metav1.OwnerReference{*tt.owner}
			}
			objects := make([]client.Object, 0, len(tt.machines)+1)
			objects = append(objects, object)
			for _, machine := range tt.machines {
				objects = append(objects, machine)
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			got, err := FindCAPIMachineForInfrastructure(t.Context(), c, object)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("FindCAPIMachineForInfrastructure() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("FindCAPIMachineForInfrastructure() error = %v", err)
			}
			if got == nil || got.Name != tt.want {
				t.Fatalf("FindCAPIMachineForInfrastructure() = %#v, want %q", got, tt.want)
			}
		})
	}
}
