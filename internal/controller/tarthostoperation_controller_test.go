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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/internal/application/driver"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/operation"
)

func TestTartHostOperationReconcilerはUpdate開始時にHostをUpdatingへ移す(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := operationTestHost()
	operation := operationTestUpdate(host)
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(host, operation).
		Build()
	hostPhase := &recordingOperationHostPhase{}
	reconciler := &TartHostOperationReconciler{
		Client:    k8sClient,
		Scheme:    scheme,
		PowerOn:   successfulOperationPowerOn{},
		HostPhase: hostPhase,
	}

	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(operation),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !hostPhase.updating {
		t.Fatal("MarkHostUpdating() was not called")
	}
	if hostPhase.provisioning {
		t.Fatal("MarkHostProvisioning() was called for Update Operation")
	}
}

func TestTartHostOperationReconcilerはRollback成功時にHostをProvisionedへ戻す(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := operationTestHost()
	host.Status.Phase = infrastructurev1beta1.TartHostPhaseUpdating
	operation := operationTestUpdate(host)
	operation.Status.Phase = infrastructurev1beta1.TartHostOperationPhaseFailed
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(host, operation).
		Build()
	hostPhase := &recordingOperationHostPhase{}
	reconciler := &TartHostOperationReconciler{
		Client:    k8sClient,
		Scheme:    scheme,
		PowerOn:   successfulOperationPowerOn{},
		HostPhase: hostPhase,
	}

	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(operation),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !hostPhase.provisioned {
		t.Fatal("MarkHostProvisioned() was not called")
	}
}

func TestTartHostOperationReconcilerはUpdateのHealthGateDeadline超過をRollbackへ切り替える(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := operationTestHost()
	operation := operationTestUpdate(host)
	operation.Spec.Deadline = metav1.NewTime(time.Now().Add(-time.Minute))
	operation.Status.Phase = infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(host, operation).
		Build()
	reconciler := &TartHostOperationReconciler{
		Client:    k8sClient,
		Scheme:    scheme,
		PowerOn:   successfulOperationPowerOn{},
		HostPhase: &recordingOperationHostPhase{},
	}

	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(operation),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	current := &infrastructurev1beta1.TartHostOperation{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(operation), current); err != nil {
		t.Fatalf("get TartHostOperation: %v", err)
	}
	if current.Status.Phase != infrastructurev1beta1.TartHostOperationPhaseRollingBack {
		t.Fatalf("phase = %q, want RollingBack", current.Status.Phase)
	}
}

type successfulOperationPowerOn struct{}

func (successfulOperationPowerOn) PowerOn(
	context.Context,
	driverdomain.Name,
	driverdomain.HostTarget,
	operationdomain.ID,
	applicationdriver.Invocation,
) error {
	return nil
}

type recordingOperationHostPhase struct {
	provisioning bool
	updating     bool
	provisioned  bool
	recovery     bool
}

func (phase *recordingOperationHostPhase) MarkHostProvisioning(
	context.Context,
	*infrastructurev1beta1.TartHost,
) error {
	phase.provisioning = true
	return nil
}

func (phase *recordingOperationHostPhase) MarkHostUpdating(
	context.Context,
	*infrastructurev1beta1.TartHost,
) error {
	phase.updating = true
	return nil
}

func (phase *recordingOperationHostPhase) MarkHostProvisioned(
	context.Context,
	*infrastructurev1beta1.TartHost,
) error {
	phase.provisioned = true
	return nil
}

func (phase *recordingOperationHostPhase) MarkHostRecoveryRequired(
	context.Context,
	*infrastructurev1beta1.TartHost,
) error {
	phase.recovery = true
	return nil
}

func (*recordingOperationHostPhase) MarkHostAvailable(
	context.Context,
	*infrastructurev1beta1.TartHost,
) error {
	return nil
}

func operationTestHost() *infrastructurev1beta1.TartHost {
	return &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "host-a",
			Namespace: "default",
			UID:       types.UID("host-a"),
		},
		Spec: infrastructurev1beta1.TartHostSpec{
			Identifiers: infrastructurev1beta1.HostIdentifiers{
				BootMACAddress: "02:00:00:00:00:01",
			},
			Management: infrastructurev1beta1.HostManagement{
				PowerDriver: "wol",
			},
		},
		Status: infrastructurev1beta1.TartHostStatus{
			Phase:           infrastructurev1beta1.TartHostPhaseProvisioned,
			LastStablePhase: infrastructurev1beta1.TartHostPhaseProvisioned,
		},
	}
}

func operationTestUpdate(
	host *infrastructurev1beta1.TartHost,
) *infrastructurev1beta1.TartHostOperation {
	return &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operation-a",
			Namespace: "default",
			UID:       types.UID("operation-a"),
		},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID: "0197d640-8d00-7a65-b67f-3f7c42a6935f",
			Type:        infrastructurev1beta1.OperationTypeUpdate,
			HostRef: infrastructurev1beta1.ResourceReference{
				Namespace: host.Namespace,
				Name:      host.Name,
				UID:       host.UID,
			},
			Deadline: metav1.NewTime(time.Now().Add(time.Hour)),
		},
		Status: infrastructurev1beta1.TartHostOperationStatus{
			Phase: infrastructurev1beta1.TartHostOperationPhasePending,
		},
	}
}
