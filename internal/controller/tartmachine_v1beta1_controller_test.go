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
	"strings"
	"testing"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	k8sallocation "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/allocation"
	appprovisioning "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning"
	appupdate "github.com/walnuts1018/cluster-api-provider-tart/internal/application/inplaceupdate"
	applicationallocation "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineallocation"
	machinehealthdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/machinehealth"
)

func TestTartMachineV1Beta1ReconcilerSetsAllocationConflict(t *testing.T) {
	t.Parallel()

	testScheme := newV1Beta1TestScheme(t)
	host := &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "host-a",
			Namespace: "default",
			UID:       types.UID("host-a-uid"),
		},
		Spec: infrastructurev1beta1.TartHostSpec{
			ConsumerRef: &infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "other-machine",
				UID:       types.UID("other-machine-uid"),
			},
		},
	}
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "machine-a",
			Namespace:  "default",
			UID:        types.UID("machine-a-uid"),
			Generation: 3,
		},
		Status: infrastructurev1beta1.TartMachineStatus{
			HostRef: &infrastructurev1beta1.ResourceReference{
				Namespace: host.Namespace,
				Name:      host.Name,
				UID:       host.UID,
			},
		},
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1beta1.TartMachine{}).
		WithObjects(machine, host).
		Build()
	reconciler := &TartMachineV1Beta1Reconciler{
		Client:         k8sClient,
		HostReferences: k8sallocation.NewService(k8sClient),
	}

	if _, err := reconciler.Reconcile(t.Context(), requestFor(machine)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	current := &infrastructurev1beta1.TartMachine{}
	if err := k8sClient.Get(
		t.Context(),
		types.NamespacedName{Namespace: machine.Namespace, Name: machine.Name},
		current,
	); err != nil {
		t.Fatalf("get TartMachine: %v", err)
	}
	condition := apimeta.FindStatusCondition(current.Status.Conditions, applicationallocation.ReadyCondition)
	if condition == nil ||
		condition.Status != metav1.ConditionFalse ||
		condition.Reason != applicationallocation.AllocationConflictReason {
		t.Fatalf("Ready condition = %#v", condition)
	}
	if current.Status.HostRef == nil || current.Status.HostRef.UID != host.UID {
		t.Fatalf("hostRef = %#v, want original TartHost reference", current.Status.HostRef)
	}
}

func TestTartMachineV1Beta1ReconcilerRepairsHostReference(t *testing.T) {
	t.Parallel()

	testScheme := newV1Beta1TestScheme(t)
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-a",
			Namespace: "default",
			UID:       types.UID("machine-a-uid"),
		},
	}
	host := &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "host-a",
			Namespace: machine.Namespace,
			UID:       types.UID("host-a-uid"),
		},
		Spec: infrastructurev1beta1.TartHostSpec{
			ConsumerRef: &infrastructurev1beta1.ResourceReference{
				Namespace: machine.Namespace,
				Name:      machine.Name,
				UID:       machine.UID,
			},
		},
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1beta1.TartMachine{}).
		WithObjects(machine, host).
		Build()
	reconciler := &TartMachineV1Beta1Reconciler{
		Client:         k8sClient,
		HostReferences: k8sallocation.NewService(k8sClient),
	}

	if _, err := reconciler.Reconcile(t.Context(), requestFor(machine)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	current := &infrastructurev1beta1.TartMachine{}
	if err := k8sClient.Get(
		t.Context(),
		types.NamespacedName{Namespace: machine.Namespace, Name: machine.Name},
		current,
	); err != nil {
		t.Fatalf("get TartMachine: %v", err)
	}
	if current.Status.HostRef == nil ||
		current.Status.HostRef.Name != host.Name ||
		current.Status.HostRef.UID != host.UID {
		t.Fatalf("hostRef = %#v, want TartHost %s", current.Status.HostRef, host.Name)
	}
}

func TestTartMachineV1Beta1ReconcilerSetsProviderIDMismatch(t *testing.T) {
	t.Parallel()

	testScheme := newV1Beta1TestScheme(t)
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "machine-a",
			Namespace:  "default",
			UID:        types.UID("machine-a-uid"),
			Generation: 2,
		},
		Spec: infrastructurev1beta1.TartMachineSpec{ProviderID: "tart://host-a"},
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1beta1.TartMachine{}).
		WithObjects(machine).
		Build()
	reconciler := &TartMachineV1Beta1Reconciler{
		Client:         k8sClient,
		HostReferences: k8sallocation.NewService(k8sClient),
		NodeHealth: nodeHealthObserverStub{
			observation: machinehealthdomain.NodeObservation{
				MachineProviderID: "tart://host-a",
				NodeProviderID:    "tart://host-b",
				NodeReady:         true,
			},
		},
	}

	if _, err := reconciler.Reconcile(t.Context(), requestFor(machine)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	current := &infrastructurev1beta1.TartMachine{}
	if err := k8sClient.Get(
		t.Context(),
		types.NamespacedName{Namespace: machine.Namespace, Name: machine.Name},
		current,
	); err != nil {
		t.Fatalf("get TartMachine: %v", err)
	}
	condition := apimeta.FindStatusCondition(current.Status.Conditions, "Ready")
	if condition == nil ||
		condition.Status != metav1.ConditionFalse ||
		condition.Reason != string(machinehealthdomain.ReasonProviderIDMismatch) {
		t.Fatalf("Ready condition = %#v", condition)
	}
}

func TestTartMachineV1Beta1ReconcilerDoesNotProvisionBeforeHealthGate(t *testing.T) {
	t.Parallel()

	testScheme := newV1Beta1TestScheme(t)
	machine, host, operation := provisioningGateObjects(
		infrastructurev1beta1.TartHostOperationPhaseBootTrial,
	)
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1beta1.TartMachine{}, &infrastructurev1beta1.TartHostOperation{}).
		WithObjects(machine, host, operation).
		Build()
	reconciler := &TartMachineV1Beta1Reconciler{
		Client:         k8sClient,
		HostReferences: k8sallocation.NewService(k8sClient),
		NodeHealth: nodeHealthObserverStub{observation: machinehealthdomain.NodeObservation{
			MachineProviderID: machine.Spec.ProviderID,
			NodeProviderID:    machine.Spec.ProviderID,
			NodeReady:         true,
			ExpectedVersion:   "v1.35.0",
			NodeVersion:       "v1.35.0",
		}},
	}

	if _, err := reconciler.Reconcile(t.Context(), requestFor(machine)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	current := &infrastructurev1beta1.TartMachine{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(machine), current); err != nil {
		t.Fatalf("get TartMachine: %v", err)
	}
	if current.Status.Initialization.Provisioned != nil && *current.Status.Initialization.Provisioned {
		t.Fatal("initialization.provisioned = true before AwaitingHealth")
	}
}

func TestTartMachineV1Beta1ReconcilerResumesOperationAfterHostReferenceRepair(t *testing.T) {
	t.Parallel()

	testScheme := newV1Beta1TestScheme(t)
	bootstrapSecretName := "machine-bootstrap"
	owner := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owner-machine",
			Namespace: "default",
			UID:       types.UID("owner-machine-uid"),
		},
		Spec: clusterv1.MachineSpec{
			Bootstrap: clusterv1.Bootstrap{DataSecretName: &bootstrapSecretName},
		},
	}
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-resume",
			Namespace: owner.Namespace,
			UID:       types.UID("machine-resume-uid"),
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: clusterv1.GroupVersion.String(),
				Kind:       "Machine",
				Name:       owner.Name,
				UID:        owner.UID,
			}},
		},
	}
	host := &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "host-resume",
			Namespace: machine.Namespace,
			UID:       types.UID("host-resume-uid"),
		},
		Spec: infrastructurev1beta1.TartHostSpec{
			ConsumerRef: &infrastructurev1beta1.ResourceReference{
				Namespace: machine.Namespace,
				Name:      machine.Name,
				UID:       machine.UID,
			},
		},
	}
	operation := &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operation-resume",
			Namespace: machine.Namespace,
			UID:       types.UID("operation-resume-uid"),
		},
	}
	provisioner := &provisionOrchestratorStub{host: host, operation: operation}
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1beta1.TartMachine{}).
		WithObjects(machine, host, owner).
		Build()
	reconciler := &TartMachineV1Beta1Reconciler{
		Client:         k8sClient,
		HostReferences: k8sallocation.NewService(k8sClient),
		Provisioner:    provisioner,
	}

	if _, err := reconciler.Reconcile(t.Context(), requestFor(machine)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if provisioner.calls != 1 {
		t.Fatalf("Start() calls = %d, want 1", provisioner.calls)
	}

	current := &infrastructurev1beta1.TartMachine{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(machine), current); err != nil {
		t.Fatalf("get TartMachine: %v", err)
	}
	if current.Status.OperationRef == nil || current.Status.OperationRef.UID != operation.UID {
		t.Fatalf("operationRef = %#v, want %q", current.Status.OperationRef, operation.UID)
	}
	if current.Spec.ProviderID != "tart://"+host.Name {
		t.Fatalf("providerID = %q, want tart://%s", current.Spec.ProviderID, host.Name)
	}
}

func TestTartMachineV1Beta1ReconcilerProvisionsAfterEveryHealthGate(t *testing.T) {
	t.Parallel()

	testScheme := newV1Beta1TestScheme(t)
	machine, host, operation := provisioningGateObjects(
		infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
	)
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1beta1.TartMachine{}, &infrastructurev1beta1.TartHostOperation{}).
		WithObjects(machine, host, operation).
		Build()
	provisioner := &provisionOrchestratorStub{}
	reconciler := &TartMachineV1Beta1Reconciler{
		Client:         k8sClient,
		HostReferences: k8sallocation.NewService(k8sClient),
		NodeHealth: nodeHealthObserverStub{observation: machinehealthdomain.NodeObservation{
			MachineProviderID: machine.Spec.ProviderID,
			NodeProviderID:    machine.Spec.ProviderID,
			NodeReady:         true,
			ExpectedVersion:   "v1.35.0",
			NodeVersion:       "v1.35.0",
		}},
		Provisioner: provisioner,
	}

	if _, err := reconciler.Reconcile(t.Context(), requestFor(machine)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	current := &infrastructurev1beta1.TartMachine{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(machine), current); err != nil {
		t.Fatalf("get TartMachine: %v", err)
	}
	if current.Status.Initialization.Provisioned == nil || !*current.Status.Initialization.Provisioned {
		t.Fatalf("initialization.provisioned = %#v, want true", current.Status.Initialization.Provisioned)
	}
	if current.Status.InstalledDistributionVersion != "v1.35.0" {
		t.Fatalf("installedDistributionVersion = %q, want v1.35.0", current.Status.InstalledDistributionVersion)
	}
	if provisioner.completeCalls != 1 {
		t.Fatalf("CompleteProvisioning() calls = %d, want 1", provisioner.completeCalls)
	}
}

func TestTartMachineV1Beta1ReconcilerKeepsAwaitingHealthUntilNodeIsReady(t *testing.T) {
	t.Parallel()

	testScheme := newV1Beta1TestScheme(t)
	machine, host, operation := provisioningGateObjects(
		infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
	)
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1beta1.TartMachine{}, &infrastructurev1beta1.TartHostOperation{}).
		WithObjects(machine, host, operation).
		Build()
	provisioner := &provisionOrchestratorStub{}
	reconciler := &TartMachineV1Beta1Reconciler{
		Client:         k8sClient,
		HostReferences: k8sallocation.NewService(k8sClient),
		NodeHealth: nodeHealthObserverStub{observation: machinehealthdomain.NodeObservation{
			MachineProviderID: machine.Spec.ProviderID,
			NodeProviderID:    machine.Spec.ProviderID,
			NodeReady:         false,
			ExpectedVersion:   "v1.35.0",
			NodeVersion:       "v1.35.0",
		}},
		Provisioner: provisioner,
	}

	if _, err := reconciler.Reconcile(t.Context(), requestFor(machine)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	currentMachine := &infrastructurev1beta1.TartMachine{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(machine), currentMachine); err != nil {
		t.Fatalf("get TartMachine: %v", err)
	}
	if currentMachine.Status.Initialization.Provisioned != nil && *currentMachine.Status.Initialization.Provisioned {
		t.Fatalf("initialization.provisioned = %#v, want nil or false", currentMachine.Status.Initialization.Provisioned)
	}
	if provisioner.completeCalls != 0 {
		t.Fatalf("CompleteProvisioning() calls = %d, want 0", provisioner.completeCalls)
	}

	currentOperation := &infrastructurev1beta1.TartHostOperation{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(operation), currentOperation); err != nil {
		t.Fatalf("get TartHostOperation: %v", err)
	}
	if currentOperation.Status.Phase != infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth {
		t.Fatalf("operation phase = %q, want AwaitingHealth", currentOperation.Status.Phase)
	}
}

func TestTartMachineV1Beta1ReconcilerKeepsReadyAfterUpdateRollback(t *testing.T) {
	t.Parallel()

	testScheme := newV1Beta1TestScheme(t)
	provisioned := true
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "machine-update",
			Namespace:  "default",
			UID:        types.UID("machine-update-uid"),
			Generation: 4,
		},
		Status: infrastructurev1beta1.TartMachineStatus{
			Initialization: infrastructurev1beta1.TartMachineInitializationStatus{Provisioned: &provisioned},
			ActiveSlot:     infrastructurev1beta1.OSSlotA,
			OperationRef: &infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "operation-update",
				UID:       types.UID("operation-update-uid"),
			},
		},
	}
	operation := &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operation-update",
			Namespace: machine.Namespace,
			UID:       types.UID("operation-update-uid"),
		},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			Type: infrastructurev1beta1.OperationTypeUpdate,
		},
		Status: infrastructurev1beta1.TartHostOperationStatus{
			Phase: infrastructurev1beta1.TartHostOperationPhaseFailed,
			Conditions: []metav1.Condition{{
				Type:    appupdate.ConditionDegraded,
				Status:  metav1.ConditionTrue,
				Reason:  "BootFailed",
				Message: "In-place OS update failed during BootTrial and the previous slot is healthy",
			}},
		},
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1beta1.TartMachine{}, &infrastructurev1beta1.TartHostOperation{}).
		WithObjects(machine, operation).
		Build()
	reconciler := &TartMachineV1Beta1Reconciler{
		Client: k8sClient,
	}

	if _, err := reconciler.Reconcile(t.Context(), requestFor(machine)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	current := &infrastructurev1beta1.TartMachine{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(machine), current); err != nil {
		t.Fatalf("get TartMachine: %v", err)
	}
	condition := apimeta.FindStatusCondition(current.Status.Conditions, "Ready")
	if condition == nil ||
		condition.Status != metav1.ConditionTrue ||
		condition.Reason != "UpdateRolledBack" {
		t.Fatalf("Ready condition = %#v", condition)
	}
	degraded := apimeta.FindStatusCondition(current.Status.Conditions, appupdate.ConditionDegraded)
	if degraded == nil || degraded.Reason != "BootFailed" {
		t.Fatalf("Degraded condition = %#v", degraded)
	}
}

func TestTartMachineV1Beta1ReconcilerEmitsUpdateFailureEvent(t *testing.T) {
	t.Parallel()

	testScheme := newV1Beta1TestScheme(t)
	provisioned := true
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "machine-update-event",
			Namespace:  "default",
			UID:        types.UID("machine-update-event-uid"),
			Generation: 2,
		},
		Status: infrastructurev1beta1.TartMachineStatus{
			Initialization: infrastructurev1beta1.TartMachineInitializationStatus{Provisioned: &provisioned},
			HostRef: &infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "host-update",
				UID:       types.UID("host-update-uid"),
			},
			OperationRef: &infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "operation-update-event",
				UID:       types.UID("operation-update-event-uid"),
			},
		},
	}
	operation := &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operation-update-event",
			Namespace: machine.Namespace,
			UID:       types.UID("operation-update-event-uid"),
		},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			OperationID: "11111111-1111-1111-1111-111111111111",
			Type:        infrastructurev1beta1.OperationTypeUpdate,
			HostRef: infrastructurev1beta1.ResourceReference{
				Namespace: machine.Namespace,
				Name:      "host-update",
				UID:       types.UID("host-update-uid"),
			},
		},
		Status: infrastructurev1beta1.TartHostOperationStatus{
			Phase: infrastructurev1beta1.TartHostOperationPhaseFailed,
			Conditions: []metav1.Condition{{
				Type:    appupdate.ConditionDegraded,
				Status:  metav1.ConditionTrue,
				Reason:  "HealthCheckFailed",
				Message: "In-place OS update failed during AwaitingHealth and the previous slot is healthy",
			}},
		},
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1beta1.TartMachine{}, &infrastructurev1beta1.TartHostOperation{}).
		WithObjects(machine, operation).
		Build()
	recorder := events.NewFakeRecorder(1)
	reconciler := &TartMachineV1Beta1Reconciler{
		Client:   k8sClient,
		Recorder: recorder,
	}

	if _, err := reconciler.Reconcile(t.Context(), requestFor(machine)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	select {
	case event := <-recorder.Events:
		for _, want := range []string{
			"UpdateFailed",
			"11111111-1111-1111-1111-111111111111",
			"host-update",
			"Update",
			"HealthCheckFailed",
		} {
			if !strings.Contains(event, want) {
				t.Fatalf("event = %q, want substring %q", event, want)
			}
		}
	default:
		t.Fatal("expected update failure event")
	}
}

func TestTartMachineV1Beta1ReconcilerDoesNotOverwriteRollbackConditionWithNodeHealth(t *testing.T) {
	t.Parallel()

	testScheme := newV1Beta1TestScheme(t)
	provisioned := true
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "machine-update-rollback-health",
			Namespace:  "default",
			UID:        types.UID("machine-update-rollback-health-uid"),
			Generation: 4,
		},
		Spec: infrastructurev1beta1.TartMachineSpec{ProviderID: "tart://host-update"},
		Status: infrastructurev1beta1.TartMachineStatus{
			Initialization: infrastructurev1beta1.TartMachineInitializationStatus{Provisioned: &provisioned},
			ActiveSlot:     infrastructurev1beta1.OSSlotA,
			OperationRef: &infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "operation-update-rollback-health",
				UID:       types.UID("operation-update-rollback-health-uid"),
			},
		},
	}
	operation := &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operation-update-rollback-health",
			Namespace: machine.Namespace,
			UID:       types.UID("operation-update-rollback-health-uid"),
		},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			Type: infrastructurev1beta1.OperationTypeUpdate,
		},
		Status: infrastructurev1beta1.TartHostOperationStatus{
			Phase: infrastructurev1beta1.TartHostOperationPhaseFailed,
		},
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1beta1.TartMachine{}, &infrastructurev1beta1.TartHostOperation{}).
		WithObjects(machine, operation).
		Build()
	reconciler := &TartMachineV1Beta1Reconciler{
		Client: k8sClient,
		NodeHealth: nodeHealthObserverStub{observation: machinehealthdomain.NodeObservation{
			MachineProviderID: machine.Spec.ProviderID,
			NodeProviderID:    machine.Spec.ProviderID,
			NodeReady:         true,
			ExpectedVersion:   "v1.35.0",
			NodeVersion:       "v1.35.0",
		}},
	}

	if _, err := reconciler.Reconcile(t.Context(), requestFor(machine)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	current := &infrastructurev1beta1.TartMachine{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(machine), current); err != nil {
		t.Fatalf("get TartMachine: %v", err)
	}
	condition := apimeta.FindStatusCondition(current.Status.Conditions, "Ready")
	if condition == nil ||
		condition.Status != metav1.ConditionTrue ||
		condition.Reason != "UpdateRolledBack" {
		t.Fatalf("Ready condition = %#v", condition)
	}
}

func TestTartMachineV1Beta1ReconcilerCompletesUpdateAfterNodeHealth(t *testing.T) {
	t.Parallel()

	testScheme := newV1Beta1TestScheme(t)
	provisioned := true
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "machine-update-ready",
			Namespace:  "default",
			UID:        types.UID("machine-update-ready-uid"),
			Generation: 5,
		},
		Spec: infrastructurev1beta1.TartMachineSpec{ProviderID: "tart://host-update"},
		Status: infrastructurev1beta1.TartMachineStatus{
			Initialization: infrastructurev1beta1.TartMachineInitializationStatus{Provisioned: &provisioned},
			ActiveSlot:     infrastructurev1beta1.OSSlotA,
			OperationRef: &infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "operation-update-ready",
				UID:       types.UID("operation-update-ready-uid"),
			},
		},
	}
	operation := &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operation-update-ready",
			Namespace: machine.Namespace,
			UID:       types.UID("operation-update-ready-uid"),
		},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			Type:              infrastructurev1beta1.OperationTypeUpdate,
			TargetSlot:        infrastructurev1beta1.OSSlotB,
			TargetImageDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		Status: infrastructurev1beta1.TartHostOperationStatus{
			Phase: infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
		},
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1beta1.TartMachine{}, &infrastructurev1beta1.TartHostOperation{}).
		WithObjects(machine, operation).
		Build()
	reconciler := &TartMachineV1Beta1Reconciler{
		Client: k8sClient,
		NodeHealth: nodeHealthObserverStub{observation: machinehealthdomain.NodeObservation{
			MachineProviderID: machine.Spec.ProviderID,
			NodeProviderID:    machine.Spec.ProviderID,
			NodeReady:         true,
			ExpectedVersion:   "v1.35.0",
			NodeVersion:       "v1.35.0",
		}},
	}

	if _, err := reconciler.Reconcile(t.Context(), requestFor(machine)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	currentOperation := &infrastructurev1beta1.TartHostOperation{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(operation), currentOperation); err != nil {
		t.Fatalf("get TartHostOperation: %v", err)
	}
	if currentOperation.Status.Phase != infrastructurev1beta1.TartHostOperationPhaseSucceeded {
		t.Fatalf("operation phase = %q, want Succeeded", currentOperation.Status.Phase)
	}
	currentMachine := &infrastructurev1beta1.TartMachine{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(machine), currentMachine); err != nil {
		t.Fatalf("get TartMachine: %v", err)
	}
	if currentMachine.Status.ActiveSlot != infrastructurev1beta1.OSSlotB ||
		currentMachine.Status.InstalledImageDigest != operation.Spec.TargetImageDigest {
		t.Fatalf("machine status = %#v, want updated slot and digest", currentMachine.Status)
	}
}

func TestTartMachineV1Beta1ReconcilerRollsBackUpdateWhenNodeHealthFails(t *testing.T) {
	t.Parallel()

	testScheme := newV1Beta1TestScheme(t)
	provisioned := true
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "machine-update-unhealthy",
			Namespace:  "default",
			UID:        types.UID("machine-update-unhealthy-uid"),
			Generation: 5,
		},
		Spec: infrastructurev1beta1.TartMachineSpec{ProviderID: "tart://host-update"},
		Status: infrastructurev1beta1.TartMachineStatus{
			Initialization: infrastructurev1beta1.TartMachineInitializationStatus{Provisioned: &provisioned},
			ActiveSlot:     infrastructurev1beta1.OSSlotA,
			OperationRef: &infrastructurev1beta1.ResourceReference{
				Namespace: "default",
				Name:      "operation-update-unhealthy",
				UID:       types.UID("operation-update-unhealthy-uid"),
			},
		},
	}
	operation := &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operation-update-unhealthy",
			Namespace: machine.Namespace,
			UID:       types.UID("operation-update-unhealthy-uid"),
		},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			Type:              infrastructurev1beta1.OperationTypeUpdate,
			TargetSlot:        infrastructurev1beta1.OSSlotB,
			TargetImageDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		Status: infrastructurev1beta1.TartHostOperationStatus{
			Phase: infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth,
		},
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(testScheme).
		WithStatusSubresource(&infrastructurev1beta1.TartMachine{}, &infrastructurev1beta1.TartHostOperation{}).
		WithObjects(machine, operation).
		Build()
	reconciler := &TartMachineV1Beta1Reconciler{
		Client: k8sClient,
		NodeHealth: nodeHealthObserverStub{observation: machinehealthdomain.NodeObservation{
			MachineProviderID: machine.Spec.ProviderID,
			NodeProviderID:    machine.Spec.ProviderID,
			NodeReady:         false,
			ExpectedVersion:   "v1.35.0",
			NodeVersion:       "v1.35.0",
		}},
	}

	if _, err := reconciler.Reconcile(t.Context(), requestFor(machine)); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	currentOperation := &infrastructurev1beta1.TartHostOperation{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(operation), currentOperation); err != nil {
		t.Fatalf("get TartHostOperation: %v", err)
	}
	if currentOperation.Status.Phase != infrastructurev1beta1.TartHostOperationPhaseRollingBack {
		t.Fatalf("operation phase = %q, want RollingBack", currentOperation.Status.Phase)
	}
}

func provisioningGateObjects(
	phase infrastructurev1beta1.TartHostOperationPhase,
) (*infrastructurev1beta1.TartMachine, *infrastructurev1beta1.TartHost, *infrastructurev1beta1.TartHostOperation) {
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-gate",
			Namespace: "default",
			UID:       types.UID("machine-gate-uid"),
		},
		Spec: infrastructurev1beta1.TartMachineSpec{ProviderID: "tart://host-gate"},
	}
	host := &infrastructurev1beta1.TartHost{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "host-gate",
			Namespace: machine.Namespace,
			UID:       types.UID("host-gate-uid"),
		},
		Spec: infrastructurev1beta1.TartHostSpec{
			ConsumerRef: &infrastructurev1beta1.ResourceReference{
				Namespace: machine.Namespace,
				Name:      machine.Name,
				UID:       machine.UID,
			},
		},
	}
	operation := &infrastructurev1beta1.TartHostOperation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operation-gate",
			Namespace: machine.Namespace,
			UID:       types.UID("operation-gate-uid"),
		},
		Spec: infrastructurev1beta1.TartHostOperationSpec{
			Type: infrastructurev1beta1.OperationTypeProvision,
		},
		Status: infrastructurev1beta1.TartHostOperationStatus{
			Phase: phase,
			LastBootReport: &infrastructurev1beta1.BootReportStatus{
				StateMounted:           true,
				DataMounted:            true,
				BootstrapApplied:       true,
				BootstrapPayloadDigest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			},
		},
	}
	machine.Status.HostRef = &infrastructurev1beta1.ResourceReference{
		Namespace: host.Namespace,
		Name:      host.Name,
		UID:       host.UID,
	}
	machine.Status.OperationRef = &infrastructurev1beta1.ResourceReference{
		Namespace: operation.Namespace,
		Name:      operation.Name,
		UID:       operation.UID,
	}
	return machine, host, operation
}

func newV1Beta1TestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	testScheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(testScheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	if err := clusterv1.AddToScheme(testScheme); err != nil {
		t.Fatalf("add Cluster API types to scheme: %v", err)
	}
	return testScheme
}

func requestFor(machine *infrastructurev1beta1.TartMachine) ctrl.Request {
	return ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: machine.Namespace,
			Name:      machine.Name,
		},
	}
}

type nodeHealthObserverStub struct {
	observation machinehealthdomain.NodeObservation
}

type provisionOrchestratorStub struct {
	host          *infrastructurev1beta1.TartHost
	operation     *infrastructurev1beta1.TartHostOperation
	calls         int
	completeCalls int
}

func (s *provisionOrchestratorStub) CompleteProvisioning(
	context.Context,
	*infrastructurev1beta1.TartHost,
	*infrastructurev1beta1.TartHostOperation,
) error {
	s.completeCalls++
	return nil
}

func (s *provisionOrchestratorStub) Start(
	context.Context,
	*infrastructurev1beta1.TartMachine,
	string,
) (appprovisioning.StartResult, error) {
	s.calls++
	return appprovisioning.Started{
		Host:      s.host,
		Operation: s.operation,
	}, nil
}

func (s nodeHealthObserverStub) Observe(
	context.Context,
	*infrastructurev1beta1.TartMachine,
) (machinehealthdomain.NodeObservation, bool, error) {
	return s.observation, true, nil
}
