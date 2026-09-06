package kubernetes

import (
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	hostusecase "github.com/walnuts1018/cluster-api-provider-tart/usecase/host"
)

func TestClaimHostUsesResourceVersionAndDoesNotOverwrite(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := &infrav1alpha1.TartHost{Name: "host-a", ResourceVersion: "1"}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(host).Build()
	repo := NewTartHostRepository(fakeClient)
	consumer := corev1.ObjectReference{APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha1", Kind: "TartMachine", Namespace: "ns", Name: "machine-a", UID: types.UID("machine-a")}

	if err := repo.ClaimHost(t.Context(), host, consumer); err != nil {
		t.Fatalf("ClaimHost() error = %v", err)
	}
	if host.Spec.ConsumerRef == nil || host.Spec.ConsumerRef.UID != consumer.UID {
		t.Fatalf("ClaimHost() did not update the caller's observed binding")
	}

	stored := &infrav1alpha1.TartHost{}
	if err := fakeClient.Get(t.Context(), client.ObjectKey{Name: "host-a"}, stored); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	other := corev1.ObjectReference{APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha1", Kind: "TartMachine", Namespace: "ns", Name: "machine-b", UID: types.UID("machine-b")}
	if err := repo.ClaimHost(t.Context(), stored, other); !errors.Is(err, hostusecase.ErrClaimConflict) {
		t.Errorf("ClaimHost() error = %v, want ErrClaimConflict", err)
	}
}

func TestClaimHostRejectsUnidentifiableConsumer(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	host := &infrav1alpha1.TartHost{}
	validConsumer := corev1.ObjectReference{APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha1", Kind: "TartMachine", Namespace: "ns", Name: "machine-a", UID: types.UID("machine-a")}

	tests := map[string]struct {
		repository TartHostRepository
		host       *infrav1alpha1.TartHost
		consumer   corev1.ObjectReference
	}{
		"nil client": {
			host:     &infrav1alpha1.TartHost{Name: "host-a"},
			consumer: validConsumer,
		},
		"nil host": {
			repository: NewTartHostRepository(fakeClient),
			consumer:   validConsumer,
		},
		"empty host name": {
			repository: NewTartHostRepository(fakeClient),
			host:       host,
			consumer:   validConsumer,
		},
		"empty consumer UID": {
			repository: NewTartHostRepository(fakeClient),
			host:       &infrav1alpha1.TartHost{Name: "host-a"},
			consumer: corev1.ObjectReference{
				APIVersion: validConsumer.APIVersion,
				Kind:       validConsumer.Kind,
				Namespace:  validConsumer.Namespace,
				Name:       validConsumer.Name,
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := tt.repository.ClaimHost(t.Context(), tt.host, tt.consumer); !errors.Is(err, hostusecase.ErrInvalidClaim) {
				t.Errorf("ClaimHost() error = %v, want ErrInvalidClaim", err)
			}
		})
	}
}

func TestClaimHostRejectsMismatchedExistingReference(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	existing := corev1.ObjectReference{APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha1", Kind: "TartMachine", Namespace: "ns", Name: "old-name", UID: types.UID("machine-a")}
	host := &infrav1alpha1.TartHost{
		Name: "host-a",
		Spec: infrav1alpha1.TartHostSpec{ConsumerRef: &existing},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(host).Build()
	repo := NewTartHostRepository(fakeClient)
	wanted := existing
	wanted.Name = "new-name"

	if err := repo.ClaimHost(t.Context(), host, wanted); !errors.Is(err, hostusecase.ErrClaimConflict) {
		t.Fatalf("ClaimHost() error = %v, want ErrClaimConflict", err)
	}
}

func TestClaimHostRetriesAfterUnrelatedResourceVersionConflict(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	stored := &infrav1alpha1.TartHost{Name: "host-a"}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(stored).Build()
	repo := NewTartHostRepository(fakeClient)
	stale := stored.DeepCopy()
	latest := &infrav1alpha1.TartHost{}
	if err := fakeClient.Get(t.Context(), client.ObjectKey{Name: stored.Name}, latest); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	latest.Labels = map[string]string{"observed": "true"}
	if err := fakeClient.Update(t.Context(), latest); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	consumer := corev1.ObjectReference{APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha1", Kind: "TartMachine", Namespace: "ns", Name: "machine-a", UID: types.UID("machine-a")}
	if err := repo.ClaimHost(t.Context(), stale, consumer); err != nil {
		t.Fatalf("ClaimHost() error = %v", err)
	}
	if stale.Spec.ConsumerRef == nil || stale.Spec.ConsumerRef.UID != consumer.UID {
		t.Fatalf("ClaimHost() did not update the caller's observed binding")
	}
	if stale.Labels["observed"] != "true" {
		t.Errorf("ClaimHost() discarded the refreshed Host state: labels = %#v", stale.Labels)
	}
}
