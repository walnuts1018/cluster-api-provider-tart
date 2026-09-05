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
	consumer := corev1.ObjectReference{Kind: "TartMachine", Namespace: "ns", Name: "machine-a", UID: types.UID("machine-a")}

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
	other := corev1.ObjectReference{Kind: "TartMachine", Namespace: "ns", Name: "machine-b", UID: types.UID("machine-b")}
	if err := Claim(t.Context(), fakeClient, stored, other); !errors.Is(err, ErrClaimConflict) {
		t.Errorf("Claim() error = %v, want ErrClaimConflict", err)
	}
}
