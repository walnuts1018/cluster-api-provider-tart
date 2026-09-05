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

package controller

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

func TestTartMachineReconcilerClaimsHostBeforeProvisioning(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := infrav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := &infrav1alpha1.TartHost{
		Name: "host-a",
		UID:  types.UID("host-a"),
		Spec: infrav1alpha1.TartHostSpec{
			ID:         "018f3c5e-5f8a-7c1b-9a2d-123456789abc",
			MACAddress: "02:00:00:00:00:01",
		},
	}
	machine := &infrav1alpha1.TartMachine{
		Namespace: "cluster-a",
		Name:      "machine-a",
		UID:       types.UID("machine-a"),
		Spec: infrav1alpha1.TartMachineSpec{
			TalosImage: infrav1alpha1.TalosImage{Version: "v1.9.0", SchematicID: "schematic"},
		},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&infrav1alpha1.TartHost{}, &infrav1alpha1.TartMachine{}).WithObjects(host, machine).Build()
	reconciler := &TartMachineReconciler{Client: fakeClient}

	for range 3 {
		if _, err := reconciler.Reconcile(t.Context(), ctrl.Request{Namespace: machine.Namespace, Name: machine.Name}); err != nil {
			t.Fatalf("Reconcile() error = %v", err)
		}
	}

	claimed := &infrav1alpha1.TartHost{}
	if err := fakeClient.Get(t.Context(), client.ObjectKey{Name: host.Name}, claimed); err != nil {
		t.Fatalf("Get(TartHost) error = %v", err)
	}
	if claimed.Spec.ConsumerRef == nil || claimed.Spec.ConsumerRef.UID != machine.UID {
		t.Fatalf("TartHost consumerRef = %#v, want Machine UID %q", claimed.Spec.ConsumerRef, machine.UID)
	}
	observed := &infrav1alpha1.TartMachine{}
	if err := fakeClient.Get(t.Context(), client.ObjectKey{Namespace: machine.Namespace, Name: machine.Name}, observed); err != nil {
		t.Fatalf("Get(TartMachine) error = %v", err)
	}
	if observed.Spec.ProviderID != "tart://host/018f3c5e-5f8a-7c1b-9a2d-123456789abc" {
		t.Errorf("ProviderID = %q, want deterministic Host-based value", observed.Spec.ProviderID)
	}
	if observed.Status.HostRef == nil || observed.Status.HostRef.Name != host.Name {
		t.Errorf("status.hostRef = %#v, want %q", observed.Status.HostRef, host.Name)
	}
}
