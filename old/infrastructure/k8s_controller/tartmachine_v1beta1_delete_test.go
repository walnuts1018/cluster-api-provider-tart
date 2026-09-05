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
	"context"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	cleaning "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/do_cleaning"
	sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"
	sharedworkflow "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/workflow"
	k8sallocation "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/repository/k8s/allocation"
	resourcefinalizer "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/resource_finalizer"
)

func TestTartMachineV1Beta1Reconcilerは削除時にCleaningOperationを開始する(t *testing.T) {
	t.Parallel()

	testScheme := newV1Beta1TestScheme(t)
	now := metav1.NewTime(time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC))
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "machine-clean",
			Namespace:         "default",
			UID:               types.UID("machine-clean-uid"),
			Finalizers:        []string{resourcefinalizer.TartMachineCleanupFinalizer},
			DeletionTimestamp: &now,
		},
		Spec: infrastructurev1beta1.TartMachineSpec{
			DeletionPolicy: infrastructurev1beta1.DeletionPolicyRetainData,
		},
		Status: infrastructurev1beta1.TartMachineStatus{
			HostRef: &infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "host-clean",
				UID:       types.UID("host-clean-uid"),
			},
		},
	}
	host := &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "host-clean",
			Namespace: "default",
			UID:       types.UID("host-clean-uid"),
		},
		Spec: infrastructurev1beta1.TartHostSpec{
			RootDeviceHints: infrastructurev1beta1.RootDeviceHints{
				MinSizeBytes: 512 * 1024 * 1024 * 1024,
			},
		},
		Status: infrastructurev1beta1.TartHostStatus{
			Phase:           infrastructurev1beta1.TartHostPhaseProvisioned,
			LastStablePhase: infrastructurev1beta1.TartHostPhaseProvisioned,
			Inventory: infrastructurev1beta1.HostInventory{
				RootDisk: infrastructurev1beta1.ObservedDisk{SizeBytes: 512 * 1024 * 1024 * 1024},
			},
		},
	}
	operation := &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "active-9f5f0c6cc0cfef8c73b6ea33ff22f41eb36f5e0c8435ed2fdb6e8d4",
			Namespace: "default",
			UID:       types.UID("operation-clean-uid"),
		},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID:          "0197d640-8d00-7a65-b67f-3f7c42a6935f",
			Type:                 infrastructurev1beta1.OperationTypeClean,
			PlanDigest:           "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			DesiredObjectsDigest: "sha256:2222222222222222222222222222222222222222222222222222222222222222",
			HostRef: infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      host.Name,
				UID:       host.UID,
			},
			MachineRef: &infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      machine.Name,
				UID:       machine.UID,
			},
			Deadline: metav1.NewTime(time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)),
		},
	}
	cleaner := &cleaningOrchestratorStub{operation: operation}
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1beta1.TartMachine{}).
		WithObjects(machine, host).
		Build()
	reconciler := &TartMachineV1Beta1Reconciler{
		Client:         k8sClient,
		HostReferences: k8sallocation.NewService(k8sClient),
		Cleaner:        cleaner,
	}

	if _, err := reconciler.Reconcile(t.Context(), requestFor(machine)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if cleaner.calls != 1 {
		t.Fatalf("StartCleaning() calls = %d, want 1", cleaner.calls)
	}

	current := &infrastructurev1beta1.TartMachine{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(machine), current); err != nil {
		t.Fatalf("get TartMachine: %v", err)
	}
	if current.Status.OperationRef == nil || current.Status.OperationRef.UID != operation.UID {
		t.Fatalf("operationRef = %#v, want %q", current.Status.OperationRef, operation.UID)
	}
	if len(current.Finalizers) != 1 || current.Finalizers[0] != resourcefinalizer.TartMachineCleanupFinalizer {
		t.Fatalf("finalizers = %v, want cleanup finalizer", current.Finalizers)
	}
}

func TestTartMachineV1Beta1ReconcilerはCleaning完了後にFinalizerを外す(t *testing.T) {
	t.Parallel()

	testScheme := newV1Beta1TestScheme(t)
	now := metav1.NewTime(time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC))
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "machine-clean-finished",
			Namespace:         "default",
			UID:               types.UID("machine-clean-finished-uid"),
			Finalizers:        []string{resourcefinalizer.TartMachineCleanupFinalizer},
			DeletionTimestamp: &now,
		},
		Status: infrastructurev1beta1.TartMachineStatus{
			HostRef: &infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "host-clean-finished",
				UID:       types.UID("host-clean-finished-uid"),
			},
			OperationRef: &infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "operation-clean-finished",
				UID:       types.UID("operation-clean-finished-uid"),
			},
		},
	}
	host := &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "host-clean-finished",
			Namespace: "default",
			UID:       types.UID("host-clean-finished-uid"),
		},
	}
	operation := &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operation-clean-finished",
			Namespace: "default",
			UID:       types.UID("operation-clean-finished-uid"),
		},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			Type: infrastructurev1beta1.OperationTypeClean,
			HostRef: infrastructurev1beta1.ResourceReference{
				Namespace: host.Namespace,
				Name:      host.Name,
				UID:       host.UID,
			},
			Deadline: now,
		},
		Status: infrastructurev1beta1.TartHostOperationStatus{
			Phase: infrastructurev1beta1.TartHostOperationPhaseSucceeded,
		},
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1beta1.TartMachine{}, &infrastructurev1beta1.TartHostOperation{}).
		WithObjects(machine, host, operation).
		Build()
	reconciler := &TartMachineV1Beta1Reconciler{
		Client:         k8sClient,
		HostReferences: k8sallocation.NewService(k8sClient),
	}

	if _, err := reconciler.Reconcile(t.Context(), requestFor(machine)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	current := &infrastructurev1beta1.TartMachine{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(machine), current); err != nil {
		if apierrors.IsNotFound(err) {
			return
		}
		t.Fatalf("get TartMachine: %v", err)
	}
	if len(current.Finalizers) != 0 {
		t.Fatalf("finalizers = %v, want none", current.Finalizers)
	}
}

type cleaningOrchestratorStub struct {
	operation *infrastructurev1beta1.TartHostOperation
	calls     int
}

func (s *cleaningOrchestratorStub) Do(
	_ context.Context,
	_ cleaning.Command,
) sharedresult.Result[cleaning.Event, sharedworkflow.Failure] {
	s.calls++
	return sharedworkflow.Succeeded[cleaning.Event](cleaning.Event{Operation: s.operation})
}
