package controller

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
)

func TestTartMachineReconcilerClaimsHostBeforeProvisioning(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := &infrav1alpha1.TartHost{
		Name: "host-a",
		UID:  types.UID("host-a"),
		Spec: infrav1alpha1.TartHostSpec{
			HostID:     mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abc").String(),
			MACAddress: mustMACAddress(t, "00:00:5e:00:53:01"),
		},
	}
	machine := &infrav1alpha1.TartMachine{
		Namespace: "cluster-a",
		Name:      "machine-a",
		UID:       types.UID("machine-a"),
		Spec: infrav1alpha1.TartMachineSpec{
			Image: infrav1alpha1.TalosImageSpec{Version: "v1.9.0", SchematicID: "schematic"},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&infrav1alpha1.TartHost{}, &infrav1alpha1.TartMachine{}).WithObjects(host, machine).Build()
	reconciler := &TartMachineReconciler{Client: fakeClient}

	for range 3 {
		if _, err := reconciler.Reconcile(t.Context(), ctrl.Request{Namespace: machine.Namespace, Name: machine.Name}); err != nil {
			t.Fatalf("Reconcile() error = %v", err)
		}
	}

	claimed := &infrav1alpha1.TartHost{}
	if err := fakeClient.Get(t.Context(), client.ObjectKey{Name: host.Name}, claimed); err != nil {
		t.Fatalf("Get(TartHost) error = %v", err)
	}
	if claimed.Spec.ConsumerRef == nil || claimed.Spec.ConsumerRef.UID != machine.UID {
		t.Fatalf("TartHost consumerRef = %#v, want Machine UID %q", claimed.Spec.ConsumerRef, machine.UID)
	}
	observed := &infrav1alpha1.TartMachine{}
	if err := fakeClient.Get(t.Context(), client.ObjectKey{Namespace: machine.Namespace, Name: machine.Name}, observed); err != nil {
		t.Fatalf("Get(TartMachine) error = %v", err)
	}
	if observed.Spec.ProviderID.String() != "tart://host/018f3c5e-5f8a-7c1b-9a2d-123456789abc" {
		t.Errorf("ProviderID = %q, want deterministic Host-based value", observed.Spec.ProviderID.String())
	}
	if observed.Status.HostRef == nil || observed.Status.HostRef.Name != host.Name {
		t.Errorf("status.hostRef = %#v, want %q", observed.Status.HostRef, host.Name)
	}
}

func TestTartMachineReconcilerDoesNotMutatePausedMachine(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := &infrav1alpha1.TartHost{
		Name: "host-a",
		Spec: infrav1alpha1.TartHostSpec{
			HostID:     mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abc").String(),
			MACAddress: mustMACAddress(t, "00:00:5e:00:53:01"),
		},
	}
	machine := &infrav1alpha1.TartMachine{
		Namespace:   "cluster-a",
		Name:        "machine-a",
		UID:         types.UID("machine-a"),
		Annotations: map[string]string{clusterv1.PausedAnnotation: "true"},
		Spec: infrav1alpha1.TartMachineSpec{
			Image: infrav1alpha1.TalosImageSpec{Version: "v1.9.0", SchematicID: "schematic"},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&infrav1alpha1.TartHost{}, &infrav1alpha1.TartMachine{}).WithObjects(host, machine).Build()
	reconciler := &TartMachineReconciler{Client: fakeClient}

	if _, err := reconciler.Reconcile(t.Context(), ctrl.Request{Namespace: machine.Namespace, Name: machine.Name}); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	claimed := &infrav1alpha1.TartHost{}
	if err := fakeClient.Get(t.Context(), client.ObjectKey{Name: host.Name}, claimed); err != nil {
		t.Fatalf("Get(TartHost) error = %v", err)
	}
	if claimed.Spec.ConsumerRef != nil {
		t.Fatalf("paused Machine claimed Host: %#v", claimed.Spec.ConsumerRef)
	}
	observed := &infrav1alpha1.TartMachine{}
	if err := fakeClient.Get(t.Context(), client.ObjectKey{Namespace: machine.Namespace, Name: machine.Name}, observed); err != nil {
		t.Fatalf("Get(TartMachine) error = %v", err)
	}
	if len(observed.Finalizers) != 0 {
		t.Errorf("paused Machine finalizers = %v, want none", observed.Finalizers)
	}
	if !observed.Spec.ProviderID.IsZero() {
		t.Errorf("paused Machine ProviderID = %q, want empty", observed.Spec.ProviderID)
	}
}

func TestTartMachineReconcilerChecksProviderIDBeforeClaim(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := &infrav1alpha1.TartHost{
		Name: "host-a",
		Spec: infrav1alpha1.TartHostSpec{
			HostID:     mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abc").String(),
			MACAddress: mustMACAddress(t, "00:00:5e:00:53:01"),
		},
	}
	machine := &infrav1alpha1.TartMachine{
		Namespace: "cluster-a",
		Name:      "machine-a",
		UID:       types.UID("machine-a"),
		Spec: infrav1alpha1.TartMachineSpec{
			ProviderID: mustProviderID(t, "tart://host/018f3c5e-5f8a-7c1b-9a2d-123456789def"),
			Image:      infrav1alpha1.TalosImageSpec{Version: "v1.9.0", SchematicID: "schematic"},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&infrav1alpha1.TartHost{}, &infrav1alpha1.TartMachine{}).WithObjects(host, machine).Build()
	reconciler := &TartMachineReconciler{Client: fakeClient}

	for range 2 {
		if _, err := reconciler.Reconcile(t.Context(), ctrl.Request{Namespace: machine.Namespace, Name: machine.Name}); err != nil {
			t.Fatalf("Reconcile() error = %v", err)
		}
	}

	claimed := &infrav1alpha1.TartHost{}
	if err := fakeClient.Get(t.Context(), client.ObjectKey{Name: host.Name}, claimed); err != nil {
		t.Fatalf("Get(TartHost) error = %v", err)
	}
	if claimed.Spec.ConsumerRef != nil {
		t.Fatalf("ProviderID mismatch claimed Host: %#v", claimed.Spec.ConsumerRef)
	}
	observed := &infrav1alpha1.TartMachine{}
	if err := fakeClient.Get(t.Context(), client.ObjectKey{Namespace: machine.Namespace, Name: machine.Name}, observed); err != nil {
		t.Fatalf("Get(TartMachine) error = %v", err)
	}
	condition := meta.FindStatusCondition(observed.Status.Conditions, infrav1alpha1.TartMachineReadyCondition)
	if condition == nil || condition.Reason != infrav1alpha1.ReasonHostMismatch {
		t.Fatalf("Ready condition = %#v, want HostMismatch", condition)
	}
}

func TestTartMachineReconcilerRetainsFinalizerWhenStatusHostRefIsMissing(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	machine := &infrav1alpha1.TartMachine{
		Namespace:         "cluster-a",
		Name:              "machine-a",
		UID:               types.UID("machine-a"),
		Finalizers:        []string{tartMachineFinalizer},
		DeletionTimestamp: new(metav1.Time),
	}
	machine.DeletionTimestamp = new(metav1.Time)
	machine.DeletionTimestamp.Time = time.Unix(1, 0)
	host := &infrav1alpha1.TartHost{
		Name: "host-a",
		Spec: infrav1alpha1.TartHostSpec{
			HostID: mustHostID(t, "018f3c5e-5f8a-7c1b-9a2d-123456789abc").String(),
			ConsumerRef: &corev1.ObjectReference{
				APIVersion: infrav1alpha1.GroupVersion.String(),
				Kind:       tartMachineKind,
				Namespace:  machine.Namespace,
				Name:       machine.Name,
				UID:        machine.UID,
			},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&infrav1alpha1.TartHost{}, &infrav1alpha1.TartMachine{}).WithObjects(host, machine).Build()
	reconciler := &TartMachineReconciler{Client: fakeClient}

	result, err := reconciler.Reconcile(t.Context(), ctrl.Request{Namespace: machine.Namespace, Name: machine.Name})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.RequeueAfter != shutdownConfirmationRequeue {
		t.Errorf("Reconcile() RequeueAfter = %s, want %s", result.RequeueAfter, shutdownConfirmationRequeue)
	}

	observed := &infrav1alpha1.TartMachine{}
	if err := fakeClient.Get(t.Context(), client.ObjectKey{Namespace: machine.Namespace, Name: machine.Name}, observed); err != nil {
		t.Fatalf("Get(TartMachine) error = %v", err)
	}
	if len(observed.Finalizers) != 1 || observed.Finalizers[0] != tartMachineFinalizer {
		t.Errorf("Machine finalizers = %v, want lifecycle finalizer retained", observed.Finalizers)
	}
	condition := meta.FindStatusCondition(observed.Status.Conditions, infrav1alpha1.TartMachineReadyCondition)
	if condition == nil || condition.Reason != infrav1alpha1.ReasonShutdownUnconfirmed {
		t.Fatalf("Ready condition = %#v, want ShutdownUnconfirmed", condition)
	}
}

func mustHostID(t *testing.T, value string) hostdomain.HostID {
	t.Helper()
	id, err := hostdomain.ParseHostID(value)
	if err != nil {
		t.Fatalf("ParseHostID() error = %v", err)
	}
	return id
}

func mustMACAddress(t *testing.T, value string) network.MACAddress {
	t.Helper()
	address, err := network.ParseMACAddress(value)
	if err != nil {
		t.Fatalf("ParseMACAddress() error = %v", err)
	}
	return address
}

func mustProviderID(t *testing.T, value string) hostdomain.ProviderID {
	t.Helper()
	providerID, err := hostdomain.ParseProviderID(value)
	if err != nil {
		t.Fatalf("ParseProviderID() error = %v", err)
	}
	return providerID
}
