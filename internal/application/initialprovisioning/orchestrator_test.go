package initialprovisioning

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	allocationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/allocation"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/domain/capability"
)

func TestRequirementsForMachineUsesExactPlatformProfileContract(t *testing.T) {
	t.Parallel()

	machine := testMachine()
	requirements, err := requirementsForMachine(machine)
	if err != nil {
		t.Fatalf("requirementsForMachine() error = %v", err)
	}
	if requirements.Architecture != "amd64" || requirements.Firmware != "UEFI" {
		t.Fatalf("requirements = %#v", requirements)
	}
	const minimumDiskBytes = 64 * 1024 * 1024 * 1024
	if requirements.MinRootDiskBytes != minimumDiskBytes {
		t.Fatalf("MinRootDiskBytes = %d, want %d", requirements.MinRootDiskBytes, minimumDiskBytes)
	}
	requiredCapabilities, err := capability.NewSet(capability.PowerOn)
	if err != nil {
		t.Fatalf("NewSet() error = %v", err)
	}
	if !requirements.Capabilities.ContainsAll(requiredCapabilities) {
		t.Fatalf("Capabilities = %#v, want PowerOn", requirements.Capabilities)
	}

	machine.Spec.PlatformProfile = "amd64-uefi-unknown/v1"
	if _, err := requirementsForMachine(machine); err == nil {
		t.Fatal("requirementsForMachine() accepted an unknown profile")
	}
}

func TestDesiredObjectsDigestIsStableAndChangesWithMachineSpec(t *testing.T) {
	t.Parallel()

	machine := testMachine()
	first, err := desiredObjectsDigest(machine)
	if err != nil {
		t.Fatalf("desiredObjectsDigest() error = %v", err)
	}
	second, err := desiredObjectsDigest(machine.DeepCopy())
	if err != nil {
		t.Fatalf("desiredObjectsDigest(copy) error = %v", err)
	}
	if first != second || first == "sha256:0000000000000000000000000000000000000000000000000000000000000000" {
		t.Fatalf("digest = %q and %q, want equal non-placeholder values", first, second)
	}

	changed := machine.DeepCopy()
	changed.Spec.Image.Ref = "oci://registry.test.walnuts.dev/os@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	changedDigest, err := desiredObjectsDigest(changed)
	if err != nil {
		t.Fatalf("desiredObjectsDigest(changed) error = %v", err)
	}
	if changedDigest == first {
		t.Fatal("digest did not change with TartMachine spec")
	}
}

func TestOrchestratorMapsNoMatchingHost(t *testing.T) {
	t.Parallel()

	orchestrator := NewOrchestrator(
		hostReserveStub{err: allocationdomain.ErrNoMatchingHost},
		hostPhaseStub{},
		operationServiceStub{},
	)
	_, _, err := orchestrator.ReserveAndStartOperation(
		t.Context(),
		testMachine(),
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)
	if !errors.Is(err, ErrNoAvailableHost) {
		t.Fatalf("ReserveAndStartOperation() error = %v, want %v", err, ErrNoAvailableHost)
	}
}

func testMachine() *infrastructurev1beta1.TartMachine {
	return &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-a",
			Namespace: "default",
			UID:       types.UID("machine-a-uid"),
		},
		Spec: infrastructurev1beta1.TartMachineSpec{
			Image: infrastructurev1beta1.ImageSpec{
				Ref: "oci://registry.test.walnuts.dev/os@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
			PlatformProfile: "amd64-uefi-ab/v1",
			HostSelector: infrastructurev1beta1.HostSelector{
				MatchLabels: map[string]string{"rack": "a"},
			},
			DeletionPolicy: infrastructurev1beta1.DeletionPolicyWipeAll,
		},
	}
}

type hostReserveStub struct {
	host *infrastructurev1beta1.TartHost
	err  error
}

func (s hostReserveStub) Reserve(
	context.Context,
	*infrastructurev1beta1.TartMachine,
	allocationdomain.Requirements,
) (*infrastructurev1beta1.TartHost, error) {
	return s.host, s.err
}

type hostPhaseStub struct {
	err error
}

func (s hostPhaseStub) ReserveForMachine(
	context.Context,
	*infrastructurev1beta1.TartHost,
	*infrastructurev1beta1.TartMachine,
) error {
	return s.err
}

func (s hostPhaseStub) MarkHostProvisioned(
	context.Context,
	*infrastructurev1beta1.TartHost,
) error {
	return s.err
}

type operationServiceStub struct {
	operation *infrastructurev1beta1.TartHostOperation
	err       error
}

func (s operationServiceStub) Start(
	context.Context,
	*infrastructurev1beta1.TartHostOperation,
) (*infrastructurev1beta1.TartHostOperation, error) {
	return s.operation, s.err
}

func (s operationServiceStub) CompleteProvision(
	context.Context,
	*infrastructurev1beta1.TartHostOperation,
) error {
	return s.err
}
