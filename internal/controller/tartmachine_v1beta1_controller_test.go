package controller

import (
	"context"
	"testing"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	k8sallocation "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/allocation"
	applicationallocation "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineallocation"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
)

func TestTartMachineV1Beta1ReconcilerSetsAllocationConflict(t *testing.T) {
	t.Parallel()

	testScheme := newV1Beta1TestScheme(t)
	host := &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "host-a",
			Namespace: "default",
			UID:       types.UID("host-a-uid"),
		},
		Spec: infrastructurev1beta1.TartHostSpec{
			ConsumerRef: &infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "other-machine",
				UID:       types.UID("other-machine-uid"),
			},
		},
	}
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "machine-a",
			Namespace:  "default",
			UID:        types.UID("machine-a-uid"),
			Generation: 3,
		},
		Status: infrastructurev1beta1.TartMachineStatus{
			HostRef: &infrastructurev1beta1.ResourceReference{
				Namespace: host.Namespace,
				Name:      host.Name,
				UID:       host.UID,
			},
		},
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1beta1.TartMachine{}).
		WithObjects(machine, host).
		Build()
	reconciler := &TartMachineV1Beta1Reconciler{
		Client:         k8sClient,
		HostReferences: k8sallocation.NewService(k8sClient),
	}

	if _, err := reconciler.Reconcile(context.Background(), requestFor(machine)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	current := &infrastructurev1beta1.TartMachine{}
	if err := k8sClient.Get(
		context.Background(),
		types.NamespacedName{Namespace: machine.Namespace, Name: machine.Name},
		current,
	); err != nil {
		t.Fatalf("get TartMachine: %v", err)
	}
	condition := apimeta.FindStatusCondition(current.Status.Conditions, applicationallocation.ReadyCondition)
	if condition == nil ||
		condition.Status != metav1.ConditionFalse ||
		condition.Reason != applicationallocation.AllocationConflictReason {
		t.Fatalf("Ready condition = %#v", condition)
	}
	if current.Status.HostRef == nil || current.Status.HostRef.UID != host.UID {
		t.Fatalf("hostRef = %#v, want original TartHost reference", current.Status.HostRef)
	}
}

func TestTartMachineV1Beta1ReconcilerRepairsHostReference(t *testing.T) {
	t.Parallel()

	testScheme := newV1Beta1TestScheme(t)
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-a",
			Namespace: "default",
			UID:       types.UID("machine-a-uid"),
		},
	}
	host := &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "host-a",
			Namespace: machine.Namespace,
			UID:       types.UID("host-a-uid"),
		},
		Spec: infrastructurev1beta1.TartHostSpec{
			ConsumerRef: &infrastructurev1beta1.ResourceReference{
				Namespace: machine.Namespace,
				Name:      machine.Name,
				UID:       machine.UID,
			},
		},
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1beta1.TartMachine{}).
		WithObjects(machine, host).
		Build()
	reconciler := &TartMachineV1Beta1Reconciler{
		Client:         k8sClient,
		HostReferences: k8sallocation.NewService(k8sClient),
	}

	if _, err := reconciler.Reconcile(context.Background(), requestFor(machine)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	current := &infrastructurev1beta1.TartMachine{}
	if err := k8sClient.Get(
		context.Background(),
		types.NamespacedName{Namespace: machine.Namespace, Name: machine.Name},
		current,
	); err != nil {
		t.Fatalf("get TartMachine: %v", err)
	}
	if current.Status.HostRef == nil ||
		current.Status.HostRef.Name != host.Name ||
		current.Status.HostRef.UID != host.UID {
		t.Fatalf("hostRef = %#v, want TartHost %s", current.Status.HostRef, host.Name)
	}
}

func TestTartMachineV1Beta1ReconcilerSetsProviderIDMismatch(t *testing.T) {
	t.Parallel()

	testScheme := newV1Beta1TestScheme(t)
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "machine-a",
			Namespace:  "default",
			UID:        types.UID("machine-a-uid"),
			Generation: 2,
		},
		Spec: infrastructurev1beta1.TartMachineSpec{ProviderID: "tart://host-a"},
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1beta1.TartMachine{}).
		WithObjects(machine).
		Build()
	reconciler := &TartMachineV1Beta1Reconciler{
		Client:         k8sClient,
		HostReferences: k8sallocation.NewService(k8sClient),
		NodeHealth: nodeHealthObserverStub{
			observation: machinehealthdomain.NodeObservation{
				MachineProviderID: "tart://host-a",
				NodeProviderID:    "tart://host-b",
				NodeReady:         true,
			},
		},
	}

	if _, err := reconciler.Reconcile(context.Background(), requestFor(machine)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	current := &infrastructurev1beta1.TartMachine{}
	if err := k8sClient.Get(
		context.Background(),
		types.NamespacedName{Namespace: machine.Namespace, Name: machine.Name},
		current,
	); err != nil {
		t.Fatalf("get TartMachine: %v", err)
	}
	condition := apimeta.FindStatusCondition(current.Status.Conditions, "Ready")
	if condition == nil ||
		condition.Status != metav1.ConditionFalse ||
		condition.Reason != string(machinehealthdomain.ReasonProviderIDMismatch) {
		t.Fatalf("Ready condition = %#v", condition)
	}
}

func newV1Beta1TestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	testScheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(testScheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	return testScheme
}

func requestFor(machine *infrastructurev1beta1.TartMachine) ctrl.Request {
	return ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: machine.Namespace,
			Name:      machine.Name,
		},
	}
}

type nodeHealthObserverStub struct {
	observation machinehealthdomain.NodeObservation
}

func (s nodeHealthObserverStub) Observe(
	context.Context,
	*infrastructurev1beta1.TartMachine,
) (machinehealthdomain.NodeObservation, bool, error) {
	return s.observation, true, nil
}
