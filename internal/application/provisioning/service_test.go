package provisioning

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
)

func TestServiceBeginUsesBootMACAndMarksProvisioning(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	host := &infrastructurev1alpha1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "host-a",
			Namespace: "default",
			UID:       types.UID("host-a-uid"),
		},
		Spec: infrastructurev1alpha1.TartHostSpec{
			MACAddress:     "00:00:5e:00:53:02",
			BootMACAddress: "00:00:5e:00:53:03",
		},
		Status: infrastructurev1alpha1.TartHostStatus{
			State: infrastructurev1alpha1.TartHostStateReserved,
		},
	}

	hostService := &fakeHostService{}
	sender := &fakeWakeOnLANSender{}
	svc := NewService(hostService, hostService, sender)

	if err := svc.Begin(ctx, host); err != nil {
		t.Fatalf("Begin returned error: %v", err)
	}

	if len(sender.sentMACAddresses) != 1 || sender.sentMACAddresses[0] != "00:00:5e:00:53:03" {
		t.Fatalf("unexpected WoL destination: %#v", sender.sentMACAddresses)
	}
	if hostService.provisioningHost != host {
		t.Fatal("host was not marked as provisioning")
	}
}

type fakeHostService struct {
	assignedHost     *infrastructurev1alpha1.TartHost
	provisioningHost *infrastructurev1alpha1.TartHost
}

func (f *fakeHostService) GetAssigned(_ context.Context, _ *infrastructurev1alpha1.TartMachine) (*infrastructurev1alpha1.TartHost, error) {
	return f.assignedHost, nil
}

func (f *fakeHostService) MarkProvisioning(_ context.Context, host *infrastructurev1alpha1.TartHost) error {
	f.provisioningHost = host
	return nil
}

func TestServiceEnsureValidatesMachineRef(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	host := &infrastructurev1alpha1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "host-a",
			Namespace: "default",
			UID:       types.UID("host-a-uid"),
		},
		Spec: infrastructurev1alpha1.TartHostSpec{
			MACAddress: "00:00:5e:00:53:02",
		},
		Status: infrastructurev1alpha1.TartHostStatus{
			State: infrastructurev1alpha1.TartHostStateReserved,
			MachineRef: &corev1.ObjectReference{
				APIVersion: infrastructurev1alpha1.GroupVersion.String(),
				Kind:       "TartMachine",
				Namespace:  "default",
				Name:       "other-machine",
				UID:        types.UID("other-machine-uid"),
			},
		},
	}

	machine := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-machine",
			Namespace: "default",
			UID:       types.UID("my-machine-uid"),
		},
		Status: infrastructurev1alpha1.TartMachineStatus{
			HostRef: &corev1.ObjectReference{
				APIVersion: infrastructurev1alpha1.GroupVersion.String(),
				Kind:       "TartHost",
				Namespace:  "default",
				Name:       "host-a",
				UID:        types.UID("host-a-uid"),
			},
		},
	}

	hostService := &fakeHostService{assignedHost: host}
	sender := &fakeWakeOnLANSender{}
	svc := NewService(hostService, hostService, sender)

	if err := svc.Ensure(ctx, machine); err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}

	if len(sender.sentMACAddresses) != 0 {
		t.Fatalf("WoL should not be sent when MachineRef does not match: %#v", sender.sentMACAddresses)
	}
	if hostService.provisioningHost != nil {
		t.Fatal("host should not be marked as provisioning when MachineRef does not match")
	}
}

func TestServiceEnsureSkipsWhenMachineRefMatches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	machine := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-machine",
			Namespace: "default",
			UID:       types.UID("my-machine-uid"),
		},
		Status: infrastructurev1alpha1.TartMachineStatus{
			HostRef: &corev1.ObjectReference{
				APIVersion: infrastructurev1alpha1.GroupVersion.String(),
				Kind:       "TartHost",
				Namespace:  "default",
				Name:       "host-a",
				UID:        types.UID("host-a-uid"),
			},
		},
	}

	host := &infrastructurev1alpha1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "host-a",
			Namespace: "default",
			UID:       types.UID("host-a-uid"),
		},
		Spec: infrastructurev1alpha1.TartHostSpec{
			MACAddress: "00:00:5e:00:53:02",
		},
		Status: infrastructurev1alpha1.TartHostStatus{
			State: infrastructurev1alpha1.TartHostStateReserved,
			MachineRef: &corev1.ObjectReference{
				APIVersion: infrastructurev1alpha1.GroupVersion.String(),
				Kind:       "TartMachine",
				Namespace:  "default",
				Name:       "my-machine",
				UID:        types.UID("my-machine-uid"),
			},
		},
	}

	hostService := &fakeHostService{assignedHost: host}
	sender := &fakeWakeOnLANSender{}
	svc := NewService(hostService, hostService, sender)

	if err := svc.Ensure(ctx, machine); err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}

	if len(sender.sentMACAddresses) != 1 {
		t.Fatalf("WoL should be sent when MachineRef matches: %#v", sender.sentMACAddresses)
	}
	if hostService.provisioningHost != host {
		t.Fatal("host should be marked as provisioning when MachineRef matches")
	}
}

func TestServiceEnsureSkipsWhenAlreadyProvisioning(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	machine := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-machine",
			Namespace: "default",
			UID:       types.UID("my-machine-uid"),
		},
		Status: infrastructurev1alpha1.TartMachineStatus{
			HostRef: &corev1.ObjectReference{
				APIVersion: infrastructurev1alpha1.GroupVersion.String(),
				Kind:       "TartHost",
				Namespace:  "default",
				Name:       "host-a",
				UID:        types.UID("host-a-uid"),
			},
		},
	}

	host := &infrastructurev1alpha1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "host-a",
			Namespace: "default",
			UID:       types.UID("host-a-uid"),
		},
		Spec: infrastructurev1alpha1.TartHostSpec{
			MACAddress: "00:00:5e:00:53:02",
		},
		Status: infrastructurev1alpha1.TartHostStatus{
			State: infrastructurev1alpha1.TartHostStateProvisioning,
			MachineRef: &corev1.ObjectReference{
				APIVersion: infrastructurev1alpha1.GroupVersion.String(),
				Kind:       "TartMachine",
				Namespace:  "default",
				Name:       "my-machine",
				UID:        types.UID("my-machine-uid"),
			},
		},
	}

	hostService := &fakeHostService{assignedHost: host}
	sender := &fakeWakeOnLANSender{}
	svc := NewService(hostService, hostService, sender)

	if err := svc.Ensure(ctx, machine); err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}

	if len(sender.sentMACAddresses) != 0 {
		t.Fatalf("WoL should not be sent when already provisioning: %#v", sender.sentMACAddresses)
	}
}

func TestServiceEnsureSkipsWhenAlreadyProvisioned(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	machine := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-machine",
			Namespace: "default",
			UID:       types.UID("my-machine-uid"),
		},
		Status: infrastructurev1alpha1.TartMachineStatus{
			HostRef: &corev1.ObjectReference{
				APIVersion: infrastructurev1alpha1.GroupVersion.String(),
				Kind:       "TartHost",
				Namespace:  "default",
				Name:       "host-a",
				UID:        types.UID("host-a-uid"),
			},
		},
	}

	host := &infrastructurev1alpha1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "host-a",
			Namespace: "default",
			UID:       types.UID("host-a-uid"),
		},
		Spec: infrastructurev1alpha1.TartHostSpec{
			MACAddress: "00:00:5e:00:53:02",
		},
		Status: infrastructurev1alpha1.TartHostStatus{
			State: infrastructurev1alpha1.TartHostStateProvisioned,
			MachineRef: &corev1.ObjectReference{
				APIVersion: infrastructurev1alpha1.GroupVersion.String(),
				Kind:       "TartMachine",
				Namespace:  "default",
				Name:       "my-machine",
				UID:        types.UID("my-machine-uid"),
			},
		},
	}

	hostService := &fakeHostService{assignedHost: host}
	sender := &fakeWakeOnLANSender{}
	svc := NewService(hostService, hostService, sender)

	if err := svc.Ensure(ctx, machine); err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}

	if len(sender.sentMACAddresses) != 0 {
		t.Fatalf("WoL should not be sent when already provisioned: %#v", sender.sentMACAddresses)
	}
}

type fakeWakeOnLANSender struct {
	sentMACAddresses []string
}

func (f *fakeWakeOnLANSender) Send(macAddress string) error {
	f.sentMACAddresses = append(f.sentMACAddresses, macAddress)
	return nil
}
