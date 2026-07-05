package agentboot

import (
	"context"
	"errors"
	"testing"
	"time"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResolverはMACに対応するActiveOperationを返す(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := testHost()
	operation := testOperation(host)
	resolver := NewResolver(fake.NewClientBuilder().WithScheme(scheme).WithObjects(host, operation).Build())

	target, err := resolver.Resolve(context.Background(), "00-00-5e-00-53-01")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if target.HostUID != string(host.UID) ||
		target.OperationUID != operation.Spec.OperationID ||
		target.PlatformProfile != host.Spec.PlatformProfile {
		t.Fatalf("Resolve() = %#v", target)
	}
}

func TestResolverは対象外HostとOperationを拒否する(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	tests := []struct {
		name      string
		mutate    func(*infrastructurev1beta1.TartHost, *infrastructurev1beta1.TartHostOperation)
		wantError error
	}{
		{name: "未知MAC", mutate: func(host *infrastructurev1beta1.TartHost, _ *infrastructurev1beta1.TartHostOperation) {
			host.Spec.Identifiers.BootMACAddress = "00:00:5e:00:53:02"
		}, wantError: ErrNotFound},
		{name: "arm64", mutate: func(host *infrastructurev1beta1.TartHost, _ *infrastructurev1beta1.TartHostOperation) {
			host.Spec.Architecture = infrastructurev1beta1.ArchitectureARM64
		}, wantError: ErrUnsupported},
		{name: "Legacy BIOS", mutate: func(host *infrastructurev1beta1.TartHost, _ *infrastructurev1beta1.TartHostOperation) {
			host.Spec.Firmware = infrastructurev1beta1.FirmwareLegacyBIOS
		}, wantError: ErrUnsupported},
		{name: "対象外Profile", mutate: func(host *infrastructurev1beta1.TartHost, _ *infrastructurev1beta1.TartHostOperation) {
			host.Spec.PlatformProfile = "amd64-uefi-ab/v2"
		}, wantError: ErrUnsupported},
		{name: "BootTrial", mutate: func(_ *infrastructurev1beta1.TartHost, operation *infrastructurev1beta1.TartHostOperation) {
			operation.Status.Phase = infrastructurev1beta1.TartHostOperationPhaseBootTrial
		}, wantError: ErrNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			host := testHost()
			operation := testOperation(host)
			test.mutate(host, operation)
			resolver := NewResolver(fake.NewClientBuilder().WithScheme(scheme).WithObjects(host, operation).Build())
			_, err := resolver.Resolve(context.Background(), "00:00:5e:00:53:01")
			if !errors.Is(err, test.wantError) {
				t.Fatalf("Resolve() error = %v, want %v", err, test.wantError)
			}
		})
	}
}

func testHost() *infrastructurev1beta1.TartHost {
	return &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{Name: "host", Namespace: "default", UID: types.UID("host-uid")},
		Spec: infrastructurev1beta1.TartHostSpec{
			Identifiers:     infrastructurev1beta1.HostIdentifiers{BootMACAddress: "00:00:5e:00:53:01"},
			Architecture:    infrastructurev1beta1.ArchitectureAMD64,
			Firmware:        infrastructurev1beta1.FirmwareUEFI,
			PlatformProfile: "amd64-uefi-ab/v1",
			RootDeviceHints: infrastructurev1beta1.RootDeviceHints{MinSizeBytes: 64 << 30},
			Management:      infrastructurev1beta1.HostManagement{PowerDriver: "wol", BootDriver: "ipxe"},
		},
	}
}

func testOperation(host *infrastructurev1beta1.TartHost) *infrastructurev1beta1.TartHostOperation {
	return &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{Name: "active", Namespace: host.Namespace},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID:          "operation-uid",
			Type:                 infrastructurev1beta1.OperationTypeProvision,
			HostRef:              infrastructurev1beta1.ResourceReference{Namespace: host.Namespace, Name: host.Name, UID: host.UID},
			PlanDigest:           "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			DesiredObjectsDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Deadline:             metav1.NewTime(time.Now().Add(time.Hour)),
		},
		Status: infrastructurev1beta1.TartHostOperationStatus{Phase: infrastructurev1beta1.TartHostOperationPhaseWaitingForAgent},
	}
}
