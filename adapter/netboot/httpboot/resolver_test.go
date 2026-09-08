package httpboot

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	domainnetboot "github.com/walnuts1018/cluster-api-provider-tart/domain/netboot"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
)

func TestNewTartHostImageResolverRequiresReader(t *testing.T) {
	t.Parallel()

	if _, err := NewTartHostImageResolver(nil); err == nil {
		t.Fatal("NewTartHostImageResolver(nil) error = nil, want error")
	}
}

func TestTartHostImageResolver(t *testing.T) {
	t.Parallel()

	const (
		macInput    = "00-00-5E-00-53-02"
		namespace   = "workloads"
		machineName = "machine-a"
	)

	tests := map[string]struct {
		host      *infrav1alpha1.TartHost
		machine   *infrav1alpha1.TartMachine
		wantImage domainnetboot.BootImage
		wantFound bool
	}{
		"解決成功": {
			host: &infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{
				MACAddress:  mustMACAddress(t, "00:00:5e:00:53:02"),
				ConsumerRef: &corev1.ObjectReference{Kind: "TartMachine", Namespace: namespace, Name: machineName},
			}},
			machine: &infrav1alpha1.TartMachine{ObjectMeta: objectMeta(namespace, machineName), Spec: infrav1alpha1.TartMachineSpec{
				Image: infrav1alpha1.TalosImageSpec{Version: "v1.14.0", SchematicID: "schematic-a"},
			}},
			wantImage: domainnetboot.BootImage{Version: "v1.14.0", SchematicID: "schematic-a"},
			wantFound: true,
		},
		"一致するhostなし": {
			host: &infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{MACAddress: mustMACAddress(t, "00:00:5e:00:53:03")}},
		},
		"consumerRefなし": {
			host: &infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{MACAddress: mustMACAddress(t, "00:00:5e:00:53:02")}},
		},
		"consumerRefのkind不一致": {
			host: &infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{
				MACAddress:  mustMACAddress(t, "00:00:5e:00:53:02"),
				ConsumerRef: &corev1.ObjectReference{Kind: "Machine", Namespace: namespace, Name: machineName},
			}},
		},
		"consumerRefのnameなし": {
			host: &infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{
				MACAddress:  mustMACAddress(t, "00:00:5e:00:53:02"),
				ConsumerRef: &corev1.ObjectReference{Kind: "TartMachine", Namespace: namespace},
			}},
		},
		"machineなし": {
			host: &infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{
				MACAddress:  mustMACAddress(t, "00:00:5e:00:53:02"),
				ConsumerRef: &corev1.ObjectReference{Kind: "TartMachine", Namespace: namespace, Name: machineName},
			}},
		},
		"image未設定": {
			host: &infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{
				MACAddress:  mustMACAddress(t, "00:00:5e:00:53:02"),
				ConsumerRef: &corev1.ObjectReference{Kind: "TartMachine", Namespace: namespace, Name: machineName},
			}},
			machine: &infrav1alpha1.TartMachine{ObjectMeta: objectMeta(namespace, machineName)},
		},
		"imageのversionなし": {
			host: &infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{
				MACAddress:  mustMACAddress(t, "00:00:5e:00:53:02"),
				ConsumerRef: &corev1.ObjectReference{Kind: "TartMachine", Namespace: namespace, Name: machineName},
			}},
			machine: &infrav1alpha1.TartMachine{ObjectMeta: objectMeta(namespace, machineName), Spec: infrav1alpha1.TartMachineSpec{
				Image: infrav1alpha1.TalosImageSpec{SchematicID: "schematic-a"},
			}},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reader := newResolverFakeClient(t, tt.host, tt.machine)
			resolver, err := NewTartHostImageResolver(reader)
			if err != nil {
				t.Fatalf("NewTartHostImageResolver() error = %v", err)
			}

			gotImage, gotFound, err := resolver.ResolveBootImage(t.Context(), macInput)
			if err != nil {
				t.Fatalf("ResolveBootImage() error = %v", err)
			}
			if gotFound != tt.wantFound || gotImage != tt.wantImage {
				t.Errorf("ResolveBootImage() = (%#v, %t), want (%#v, %t)", gotImage, gotFound, tt.wantImage, tt.wantFound)
			}
		})
	}
}

func TestTartHostImageResolverRejectsInvalidMAC(t *testing.T) {
	t.Parallel()

	reader := newResolverFakeClient(t, nil, nil)
	resolver, err := NewTartHostImageResolver(reader)
	if err != nil {
		t.Fatalf("NewTartHostImageResolver() error = %v", err)
	}
	_, found, err := resolver.ResolveBootImage(t.Context(), "not-a-mac")
	if !errors.Is(err, network.ErrInvalidMACAddress) {
		t.Fatalf("ResolveBootImage() error = %v, want ErrInvalidMACAddress", err)
	}
	if found {
		t.Fatal("ResolveBootImage() found = true for invalid MAC")
	}
}

func TestTartHostImageResolverPropagatesReaderErrors(t *testing.T) {
	t.Parallel()

	listErr := errors.New("list failed")
	resolver, err := NewTartHostImageResolver(failingReader{listErr: listErr})
	if err != nil {
		t.Fatalf("NewTartHostImageResolver() error = %v", err)
	}
	_, _, err = resolver.ResolveBootImage(t.Context(), "00:00:5e:00:53:02")
	if !errors.Is(err, listErr) {
		t.Fatalf("ResolveBootImage() list error = %v, want %v", err, listErr)
	}

	getErr := errors.New("get failed")
	reader := newResolverFakeClient(t,
		&infrav1alpha1.TartHost{Spec: infrav1alpha1.TartHostSpec{
			MACAddress:  mustMACAddress(t, "00:00:5e:00:53:02"),
			ConsumerRef: &corev1.ObjectReference{Kind: "TartMachine", Namespace: "workloads", Name: "machine-a"},
		}}, nil)
	resolver, err = NewTartHostImageResolver(failingReader{Reader: reader, getErr: getErr})
	if err != nil {
		t.Fatalf("NewTartHostImageResolver() error = %v", err)
	}
	_, _, err = resolver.ResolveBootImage(t.Context(), "00:00:5e:00:53:02")
	if !errors.Is(err, getErr) {
		t.Fatalf("ResolveBootImage() get error = %v, want %v", err, getErr)
	}
}

type failingReader struct {
	client.Reader
	listErr error
	getErr  error
}

func (r failingReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if r.listErr != nil {
		return r.listErr
	}
	return r.Reader.List(ctx, list, opts...)
}

func (r failingReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if r.getErr != nil {
		return r.getErr
	}
	return r.Reader.Get(ctx, key, obj, opts...)
}

func newResolverFakeClient(t *testing.T, host *infrav1alpha1.TartHost, machine *infrav1alpha1.TartMachine) client.Reader {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	objects := make([]client.Object, 0, 2)
	if host != nil {
		objects = append(objects, host)
	}
	if machine != nil {
		objects = append(objects, machine)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func mustMACAddress(t *testing.T, value string) network.MACAddress {
	t.Helper()

	mac, err := network.ParseMACAddress(value)
	if err != nil {
		t.Fatalf("ParseMACAddress() error = %v", err)
	}
	return mac
}

func objectMeta(namespace, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: namespace, Name: name}
}
