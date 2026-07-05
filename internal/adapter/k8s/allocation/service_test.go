package allocation

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	allocationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/allocation"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
)

func TestServiceReserveAllowsOneOfOneHundredConcurrentMachines(t *testing.T) {
	ctx := t.Context()
	testScheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(testScheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}

	host := matchingHost()
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(host).
		Build()
	service := NewService(k8sClient)
	requirements := matchingRequirements(t)

	const goroutines = 100
	var successes atomic.Int32
	var unexpectedErrors atomic.Int32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func() {
			defer wg.Done()
			machine := concurrentMachine(i)
			_, err := service.Reserve(ctx, machine, requirements)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrNoMatchingHost):
			default:
				unexpectedErrors.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := successes.Load(); got != 1 {
		t.Fatalf("successful reservations = %d, want 1", got)
	}
	if got := unexpectedErrors.Load(); got != 0 {
		t.Fatalf("unexpected errors = %d, want 0", got)
	}

	current := &infrastructurev1beta1.TartHost{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: host.Namespace, Name: host.Name}, current); err != nil {
		t.Fatalf("get reserved TartHost: %v", err)
	}
	if current.Spec.ConsumerRef == nil || current.Spec.ConsumerRef.UID == "" {
		t.Fatalf("consumerRef = %#v, want a persisted machine UID", current.Spec.ConsumerRef)
	}
}

func concurrentMachine(index int) *infrastructurev1beta1.TartMachine {
	return &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("machine-%03d", index),
			Namespace: "default",
			UID:       types.UID(fmt.Sprintf("machine-%03d-uid", index)),
		},
	}
}

func TestServiceEnsureMachineHostReferenceRepairsFromConsumerRef(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	testScheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(testScheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-a",
			Namespace: "default",
			UID:       types.UID("machine-a-uid"),
		},
	}
	host := matchingHost()
	host.Spec.ConsumerRef = &infrastructurev1beta1.ResourceReference{
		Namespace: machine.Namespace,
		Name:      machine.Name,
		UID:       machine.UID,
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1beta1.TartMachine{}).
		WithObjects(machine, host).
		Build()

	result, err := NewService(k8sClient).EnsureMachineHostReference(ctx, machine)
	if err != nil {
		t.Fatalf("EnsureMachineHostReference() error = %v", err)
	}
	if result != ReferenceRepaired {
		t.Fatalf("EnsureMachineHostReference() result = %q, want %q", result, ReferenceRepaired)
	}

	current := &infrastructurev1beta1.TartMachine{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: machine.Namespace, Name: machine.Name}, current); err != nil {
		t.Fatalf("get repaired TartMachine: %v", err)
	}
	if current.Status.HostRef == nil ||
		current.Status.HostRef.Name != host.Name ||
		current.Status.HostRef.UID != host.UID {
		t.Fatalf("hostRef = %#v, want reference to TartHost %s", current.Status.HostRef, host.Name)
	}
}

func TestServiceReserveReturnsHostAlreadyClaimedBySameMachine(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	testScheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(testScheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	machine := concurrentMachine(1)
	host := matchingHost()
	host.Spec.ConsumerRef = &infrastructurev1beta1.ResourceReference{
		Namespace: machine.Namespace,
		Name:      machine.Name,
		UID:       machine.UID,
	}
	host.Status.Phase = infrastructurev1beta1.TartHostPhaseReserved
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithObjects(host).
		Build()

	got, err := NewService(k8sClient).Reserve(ctx, machine, matchingRequirements(t))
	if err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	if got.UID != host.UID {
		t.Fatalf("Reserve() UID = %q, want %q", got.UID, host.UID)
	}
}

func TestServiceEnsureMachineHostReferenceRejectsDifferentConsumerUID(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	testScheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(testScheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := matchingHost()
	host.Spec.ConsumerRef = &infrastructurev1beta1.ResourceReference{
		Namespace: "default",
		Name:      "machine-a",
		UID:       types.UID("different-machine-uid"),
	}
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-a",
			Namespace: "default",
			UID:       types.UID("machine-a-uid"),
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

	_, err := NewService(k8sClient).EnsureMachineHostReference(ctx, machine)
	if !errors.Is(err, ErrAllocationConflict) {
		t.Fatalf("EnsureMachineHostReference() error = %v, want %v", err, ErrAllocationConflict)
	}

	current := &infrastructurev1beta1.TartMachine{}
	if getErr := k8sClient.Get(ctx, types.NamespacedName{Namespace: machine.Namespace, Name: machine.Name}, current); getErr != nil {
		t.Fatalf("get TartMachine after conflict: %v", getErr)
	}
	if current.Status.HostRef == nil || current.Status.HostRef.UID != host.UID {
		t.Fatalf("hostRef was overwritten after conflict: %#v", current.Status.HostRef)
	}
}

func matchingRequirements(t *testing.T) allocationdomain.Requirements {
	t.Helper()
	requirements, err := allocationdomain.NewRequirements(
		"amd64",
		"UEFI",
		"amd64-uefi-ab/v1",
		256_000_000_000,
		[]capability.Capability{capability.PowerOn, capability.SetNextBoot},
		map[string]string{"rack": "a"},
	)
	if err != nil {
		t.Fatalf("NewRequirements() error = %v", err)
	}
	return requirements
}

func matchingHost() *infrastructurev1beta1.TartHost {
	return &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "host-a",
			Namespace:       "default",
			UID:             types.UID("host-a-uid"),
			ResourceVersion: "1",
			Labels:          map[string]string{"rack": "a"},
		},
		Spec: infrastructurev1beta1.TartHostSpec{
			Identifiers: infrastructurev1beta1.HostIdentifiers{
				BootMACAddress: "02:00:00:00:00:01",
			},
			Architecture:    infrastructurev1beta1.ArchitectureAMD64,
			Firmware:        infrastructurev1beta1.FirmwareUEFI,
			PlatformProfile: "amd64-uefi-ab/v1",
			RootDeviceHints: infrastructurev1beta1.RootDeviceHints{
				MinSizeBytes: 256_000_000_000,
			},
			Management: infrastructurev1beta1.HostManagement{
				PowerDriver: "wake-on-lan",
				BootDriver:  "pxe",
			},
		},
		Status: infrastructurev1beta1.TartHostStatus{
			Phase: infrastructurev1beta1.TartHostPhaseAvailable,
			Capabilities: []infrastructurev1beta1.Capability{
				infrastructurev1beta1.CapabilityPowerOn,
				infrastructurev1beta1.CapabilitySetNextBoot,
			},
			Inventory: infrastructurev1beta1.HostInventory{
				RootDisk: infrastructurev1beta1.ObservedDisk{SizeBytes: 512_000_000_000},
			},
		},
	}
}
