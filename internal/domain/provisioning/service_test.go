package provisioning

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
)

func TestServiceBeginUsesBootMACAndMarksProvisioning(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testScheme := runtime.NewScheme()
	if err := scheme.AddToScheme(testScheme); err != nil {
		t.Fatalf("failed to add core scheme: %v", err)
	}
	if err := infrastructurev1alpha1.AddToScheme(testScheme); err != nil {
		t.Fatalf("failed to add infrastructure scheme: %v", err)
	}

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

	fakeClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1alpha1.TartHost{}).
		WithObjects(host).
		Build()

	hostService := hostdomain.NewService(fakeClient)
	sender := &fakeWakeOnLANSender{}
	svc := NewService(fakeClient, hostService, sender)

	if err := svc.Begin(ctx, host); err != nil {
		t.Fatalf("Begin returned error: %v", err)
	}

	if len(sender.sentMACAddresses) != 1 || sender.sentMACAddresses[0] != "00:11:22:33:44:66" {
		t.Fatalf("unexpected WoL destination: %#v", sender.sentMACAddresses)
	}

	updatedHost := &infrastructurev1alpha1.TartHost{}
	if err := fakeClient.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: host.Namespace}, updatedHost); err != nil {
		t.Fatalf("failed to get updated host: %v", err)
	}
	if updatedHost.Status.State != infrastructurev1alpha1.TartHostStateProvisioning {
		t.Fatalf("host state = %s, want %s", updatedHost.Status.State, infrastructurev1alpha1.TartHostStateProvisioning)
	}
}

type fakeWakeOnLANSender struct {
	sentMACAddresses []string
}

func (f *fakeWakeOnLANSender) Send(macAddress string) error {
	f.sentMACAddresses = append(f.sentMACAddresses, macAddress)
	return nil
}
