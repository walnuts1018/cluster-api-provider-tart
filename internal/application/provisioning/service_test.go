package provisioning

import (
	"context"
	"testing"

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
			MACAddress:     "00:11:22:33:44:55",
			BootMACAddress: "00:11:22:33:44:66",
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

	if len(sender.sentMACAddresses) != 1 || sender.sentMACAddresses[0] != "00:11:22:33:44:66" {
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

type fakeWakeOnLANSender struct {
	sentMACAddresses []string
}

func (f *fakeWakeOnLANSender) Send(macAddress string) error {
	f.sentMACAddresses = append(f.sentMACAddresses, macAddress)
	return nil
}
