package host

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	apiutil "k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
)

func TestServiceReserveAvailableContinuesOnConflict(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	testScheme := runtime.NewScheme()
	if err := scheme.AddToScheme(testScheme); err != nil {
		t.Fatalf("failed to add core scheme: %v", err)
	}
	if err := infrastructurev1alpha1.AddToScheme(testScheme); err != nil {
		t.Fatalf("failed to add infrastructure scheme: %v", err)
	}

	machine := &infrastructurev1alpha1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-a",
			Namespace: "default",
			UID:       types.UID("machine-a-uid"),
		},
	}

	firstHost := &infrastructurev1alpha1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "host-a",
			Namespace:       "default",
			UID:             types.UID("host-a-uid"),
			ResourceVersion: "1",
		},
		Status: infrastructurev1alpha1.TartHostStatus{
			State: infrastructurev1alpha1.TartHostStateAvailable,
		},
	}
	secondHost := &infrastructurev1alpha1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "host-b",
			Namespace:       "default",
			UID:             types.UID("host-b-uid"),
			ResourceVersion: "1",
		},
		Status: infrastructurev1alpha1.TartHostStatus{
			State: infrastructurev1alpha1.TartHostStateAvailable,
		},
	}

	baseClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1alpha1.TartHost{}, &infrastructurev1alpha1.TartMachine{}).
		WithObjects(machine, firstHost, secondHost).
		Build()

	svc := NewService(&conflictOnFirstHostStatusClient{
		Client:       baseClient,
		conflictHost: types.NamespacedName{Name: firstHost.Name, Namespace: firstHost.Namespace},
		winningMachine: &corev1.ObjectReference{
			APIVersion: infrastructurev1alpha1.GroupVersion.String(),
			Kind:       "TartMachine",
			Namespace:  "default",
			Name:       "machine-b",
			UID:        types.UID("machine-b-uid"),
		},
	})

	reservedHost, err := svc.ReserveAvailable(ctx, machine)
	if err != nil {
		t.Fatalf("ReserveAvailable returned error: %v", err)
	}
	if reservedHost == nil {
		t.Fatal("ReserveAvailable returned nil host")
	}
	if reservedHost.Name != secondHost.Name {
		t.Fatalf("reserved wrong host: got %s want %s", reservedHost.Name, secondHost.Name)
	}

	updatedFirstHost := &infrastructurev1alpha1.TartHost{}
	if err := baseClient.Get(ctx, types.NamespacedName{Name: firstHost.Name, Namespace: firstHost.Namespace}, updatedFirstHost); err != nil {
		t.Fatalf("failed to get first host: %v", err)
	}
	if updatedFirstHost.Status.MachineRef == nil || updatedFirstHost.Status.MachineRef.Name != "machine-b" {
		t.Fatalf("first host should remain reserved by competing machine: %#v", updatedFirstHost.Status.MachineRef)
	}

	updatedSecondHost := &infrastructurev1alpha1.TartHost{}
	if err := baseClient.Get(ctx, types.NamespacedName{Name: secondHost.Name, Namespace: secondHost.Namespace}, updatedSecondHost); err != nil {
		t.Fatalf("failed to get second host: %v", err)
	}
	if updatedSecondHost.Status.State != infrastructurev1alpha1.TartHostStateReserved {
		t.Fatalf("second host state = %s, want %s", updatedSecondHost.Status.State, infrastructurev1alpha1.TartHostStateReserved)
	}
	if updatedSecondHost.Status.MachineRef == nil || updatedSecondHost.Status.MachineRef.Name != machine.Name {
		t.Fatalf("second host should be reserved by target machine: %#v", updatedSecondHost.Status.MachineRef)
	}
}

type conflictOnFirstHostStatusClient struct {
	client.Client
	conflictHost   types.NamespacedName
	winningMachine *corev1.ObjectReference
	conflicted     bool
}

func (c *conflictOnFirstHostStatusClient) Status() client.SubResourceWriter {
	return &conflictOnFirstHostStatusWriter{
		SubResourceWriter: c.Client.Status(),
		client:            c.Client,
		conflictHost:      c.conflictHost,
		winningMachine:    c.winningMachine,
		conflicted:        &c.conflicted,
	}
}

type conflictOnFirstHostStatusWriter struct {
	client.SubResourceWriter
	client         client.Client
	conflictHost   types.NamespacedName
	winningMachine *corev1.ObjectReference
	conflicted     *bool
}

func (w *conflictOnFirstHostStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return w.conflictOnce(ctx, obj, func() error {
		return w.SubResourceWriter.Update(ctx, obj, opts...)
	})
}

func (w *conflictOnFirstHostStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	return w.conflictOnce(ctx, obj, func() error {
		return w.SubResourceWriter.Patch(ctx, obj, patch, opts...)
	})
}

func TestServiceMarkProvisioned(t *testing.T) {
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
			Name:            "test-host",
			Namespace:       "default",
			UID:             types.UID("test-host-uid"),
			ResourceVersion: "1",
		},
		Status: infrastructurev1alpha1.TartHostStatus{
			State: infrastructurev1alpha1.TartHostStateProvisioning,
			MachineRef: &corev1.ObjectReference{
				APIVersion: infrastructurev1alpha1.GroupVersion.String(),
				Kind:       "TartMachine",
				Namespace:  "default",
				Name:       "machine-a",
				UID:        types.UID("machine-a-uid"),
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1alpha1.TartHost{}).
		WithObjects(host).
		Build()

	svc := NewService(client)

	err := svc.MarkProvisioned(ctx, host)
	if err != nil {
		t.Fatalf("MarkProvisioned returned error: %v", err)
	}

	updatedHost := &infrastructurev1alpha1.TartHost{}
	if err := client.Get(ctx, types.NamespacedName{Name: host.Name, Namespace: host.Namespace}, updatedHost); err != nil {
		t.Fatalf("failed to get host: %v", err)
	}
	if updatedHost.Status.State != infrastructurev1alpha1.TartHostStateProvisioned {
		t.Fatalf("host state = %s, want %s", updatedHost.Status.State, infrastructurev1alpha1.TartHostStateProvisioned)
	}
}

func (w *conflictOnFirstHostStatusWriter) conflictOnce(ctx context.Context, obj client.Object, next func() error) error {
	host, ok := obj.(*infrastructurev1alpha1.TartHost)
	if ok && !*w.conflicted && client.ObjectKeyFromObject(host) == w.conflictHost {
		if err := apiutil.RetryOnConflict(apiutil.DefaultRetry, func() error {
			latest := &infrastructurev1alpha1.TartHost{}
			if err := w.client.Get(ctx, w.conflictHost, latest); err != nil {
				return err
			}
			latest.Status.State = infrastructurev1alpha1.TartHostStateReserved
			latest.Status.MachineRef = w.winningMachine
			return w.client.Status().Update(ctx, latest)
		}); err != nil {
			return err
		}
		*w.conflicted = true
		return errors.NewConflict(
			schema.GroupResource{Group: infrastructurev1alpha1.GroupVersion.Group, Resource: "tarthosts/status"},
			host.Name,
			nil,
		)
	}
	return next()
}
