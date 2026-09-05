// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package host

import (
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

func TestClaimUsesResourceVersionAndDoesNotOverwrite(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := &infrav1alpha1.TartHost{Name: "host-a", ResourceVersion: "1"}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(host).Build()
	consumer := corev1.ObjectReference{APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha1", Kind: "TartMachine", Namespace: "ns", Name: "machine-a", UID: types.UID("machine-a")}

	if err := Claim(t.Context(), fakeClient, host, consumer); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if host.Spec.ConsumerRef == nil || host.Spec.ConsumerRef.UID != consumer.UID {
		t.Fatalf("Claim() did not update the caller's observed binding")
	}

	stored := &infrav1alpha1.TartHost{}
	if err := fakeClient.Get(t.Context(), client.ObjectKey{Name: "host-a"}, stored); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	other := corev1.ObjectReference{APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha1", Kind: "TartMachine", Namespace: "ns", Name: "machine-b", UID: types.UID("machine-b")}
	if err := Claim(t.Context(), fakeClient, stored, other); !errors.Is(err, ErrClaimConflict) {
		t.Errorf("Claim() error = %v, want ErrClaimConflict", err)
	}
}

func TestClaimRejectsUnidentifiableConsumer(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	host := &infrav1alpha1.TartHost{}
	validConsumer := corev1.ObjectReference{APIVersion: "infrastructure.cluster.x-k8s.io/v1alpha1", Kind: "TartMachine", Namespace: "ns", Name: "machine-a", UID: types.UID("machine-a")}

	tests := map[string]struct {
		client   client.Client
		host     *infrav1alpha1.TartHost
		consumer corev1.ObjectReference
	}{
		"nil client": {
			host:     &infrav1alpha1.TartHost{Name: "host-a"},
			consumer: validConsumer,
		},
		"nil host": {
			client:   fakeClient,
			consumer: validConsumer,
		},
		"empty host name": {
			client:   fakeClient,
			host:     host,
			consumer: validConsumer,
		},
		"empty consumer UID": {
			client: fakeClient,
			host:   &infrav1alpha1.TartHost{Name: "host-a"},
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

			if err := Claim(t.Context(), tt.client, tt.host, tt.consumer); !errors.Is(err, ErrInvalidClaim) {
				t.Errorf("Claim() error = %v, want ErrInvalidClaim", err)
			}
		})
	}
}

func TestClaimRejectsMismatchedExistingReference(t *testing.T) {
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
	wanted := existing
	wanted.Name = "new-name"

	if err := Claim(t.Context(), fakeClient, host, wanted); !errors.Is(err, ErrClaimConflict) {
		t.Fatalf("Claim() error = %v, want ErrClaimConflict", err)
	}
}
