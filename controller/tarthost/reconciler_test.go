package tarthost

import (
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/host"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/network"
)

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

func TestRecordBootAttemptMaintainsBoundedObservedHistory(t *testing.T) {
	t.Parallel()

	first := metav1.NewTime(time.Unix(10, 0))
	second := metav1.NewTime(time.Unix(20, 0))
	attempts := []infrav1alpha1.BootAttempt{{BootID: "boot-old", FirstObservedAt: first, LastObservedAt: first}}
	inventory := talos.Inventory{BootID: "boot-new"}
	attempts = recordBootAttempt(attempts, inventory, "192.0.2.10:50000", second)
	attempts = recordBootAttempt(attempts, inventory, "192.0.2.11:50000", metav1.NewTime(time.Unix(30, 0)))
	if len(attempts) != 2 {
		t.Fatalf("recordBootAttempt() returned %d attempts, want 2", len(attempts))
	}
	if attempts[1].BootID != "boot-new" || attempts[1].FirstObservedAt != second || attempts[1].Endpoint != "192.0.2.11:50000" {
		t.Fatalf("recordBootAttempt() new attempt = %#v", attempts[1])
	}
	if attempts[1].LastObservedAt.Unix() != 30 {
		t.Fatalf("recordBootAttempt() lastObservedAt = %s, want unix 30", attempts[1].LastObservedAt.Time)
	}

	for index := range maxBootAttempts + 3 {
		attempts = recordBootAttempt(attempts, talos.Inventory{BootID: fmt.Sprintf("boot-%d", index)}, "192.0.2.20:50000", second)
	}
	if len(attempts) != maxBootAttempts {
		t.Fatalf("recordBootAttempt() returned %d attempts after overflow, want %d", len(attempts), maxBootAttempts)
	}
	if attempts[0].BootID != "boot-3" {
		t.Fatalf("recordBootAttempt() oldest retained boot = %q, want boot-3", attempts[0].BootID)
	}
}

func TestDeletionApproved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec infrav1alpha1.TartHostSpec
		want bool
	}{
		{name: "fresh host does not need approval", want: true},
		{
			name: "claimed host needs matching consumer approval",
			spec: infrav1alpha1.TartHostSpec{
				ConsumerRef:      &corev1.ObjectReference{UID: types.UID("consumer")},
				DeletionApproval: &infrav1alpha1.DeletionApproval{ConsumerUID: types.UID("other")},
			},
		},
		{
			name: "claimed host with empty consumer UID cannot be deleted",
			spec: infrav1alpha1.TartHostSpec{
				ConsumerRef:      &corev1.ObjectReference{},
				DeletionApproval: &infrav1alpha1.DeletionApproval{},
			},
		},
		{
			name: "retained host needs matching previous-consumer approval",
			spec: infrav1alpha1.TartHostSpec{
				PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{UID: types.UID("previous")},
				DeletionApproval:    &infrav1alpha1.DeletionApproval{PreviousConsumerUID: types.UID("previous")},
			},
			want: true,
		},
		{
			name: "retained host with empty previous-consumer UID cannot be deleted",
			spec: infrav1alpha1.TartHostSpec{
				PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{},
				DeletionApproval:    &infrav1alpha1.DeletionApproval{},
			},
		},
		{
			name: "both bindings require both approvals",
			spec: infrav1alpha1.TartHostSpec{
				ConsumerRef:         &corev1.ObjectReference{UID: types.UID("consumer")},
				PreviousConsumerRef: &infrav1alpha1.PreviousConsumerRef{UID: types.UID("previous")},
				DeletionApproval: &infrav1alpha1.DeletionApproval{
					ConsumerUID:         types.UID("consumer"),
					PreviousConsumerUID: types.UID("previous"),
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := deletionApproved(tt.spec); got != tt.want {
				t.Errorf("deletionApproved() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestTartHostReconcilerReportsIdentityConflictForEveryRelatedHost(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	hosts := make([]*infrav1alpha1.TartHost, 3)
	for index := range hosts {
		hosts[index] = &infrav1alpha1.TartHost{
			Name: fmt.Sprintf("host-%d", index),
			Spec: infrav1alpha1.TartHostSpec{
				HostID:     mustHostID(t, fmt.Sprintf("018f3c5e-5f8a-7c1b-9a2d-123456789ab%d", index)).String(),
				MACAddress: mustMACAddress(t, "00:00:5e:00:53:01"),
			},
		}
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&infrav1alpha1.TartHost{}).WithObjects(hosts[0], hosts[1], hosts[2]).Build()
	reconciler := &TartHostReconciler{Client: fakeClient}

	for range 2 {
		if _, err := reconciler.Reconcile(t.Context(), ctrl.Request{Name: hosts[0].Name}); err != nil {
			t.Fatalf("Reconcile() error = %v", err)
		}
	}

	for index := range hosts {
		observed := &infrav1alpha1.TartHost{}
		if err := fakeClient.Get(t.Context(), client.ObjectKey{Name: hosts[index].Name}, observed); err != nil {
			t.Fatalf("Get(TartHost) error = %v", err)
		}
		condition := meta.FindStatusCondition(observed.Status.Conditions, infrav1alpha1.TartHostReadyCondition)
		if condition == nil || condition.Reason != infrav1alpha1.ReasonIdentityConflict {
			t.Errorf("%s Ready condition = %#v, want IdentityConflict", observed.Name, condition)
		}
	}
}
