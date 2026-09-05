package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	capabilitydomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/capability"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	appupdate "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/update_machine"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/driver"
	operationdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/operation"
	driverstateadapter "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/repository/k8s/driverstate"
	v1beta1hostadapter "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/repository/k8s/v1beta1host"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/driver"
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
		Client:             k8sClient,
		Scheme:             scheme,
		PowerOn:            successfulOperationPowerOn{},
		PrepareBoot:        &recordingOperationBootPreparation{},
		HostPhase:          hostPhase,
		DriverCapabilities: &recordingOperationDriverCapabilities{},
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

func TestTartHostOperationReconcilerは起動対象を公開してからPowerOnする(t *testing.T) {
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
	powerOn := &phaseRecordingOperationPowerOn{
		client: k8sClient,
		key:    client.ObjectKeyFromObject(operation),
	}
	reconciler := &TartHostOperationReconciler{
		Client:             k8sClient,
		Scheme:             scheme,
		PowerOn:            powerOn,
		PrepareBoot:        &recordingOperationBootPreparation{},
		HostPhase:          &recordingOperationHostPhase{},
		DriverCapabilities: &recordingOperationDriverCapabilities{},
	}

	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(operation)}
	if _, err := reconciler.Reconcile(t.Context(), request); err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}
	if powerOn.calls != 0 {
		t.Fatalf("PowerOn() calls after boot preparation = %d, want 0", powerOn.calls)
	}
	if _, err := reconciler.Reconcile(t.Context(), request); err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}
	if powerOn.calls != 1 {
		t.Fatalf("PowerOn() calls = %d, want 1", powerOn.calls)
	}
	if powerOn.observedPhase != infrastructurev1beta1.TartHostOperationPhasePreparingBoot {
		t.Fatalf("phase observed by PowerOn() = %q, want PreparingBoot", powerOn.observedPhase)
	}
	current := &infrastructurev1beta1.TartHostOperation{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(operation), current); err != nil {
		t.Fatalf("get TartHostOperation: %v", err)
	}
	if current.Status.Phase != infrastructurev1beta1.TartHostOperationPhaseWaitingForAgent {
		t.Fatalf("phase = %q, want WaitingForAgent", current.Status.Phase)
	}
}

func TestTartHostOperationReconcilerはPowerOn前にDriverCapabilitiesを観測する(t *testing.T) {
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
	observer := &recordingOperationDriverCapabilities{}
	reconciler := &TartHostOperationReconciler{
		Client:             k8sClient,
		Scheme:             scheme,
		PowerOn:            successfulOperationPowerOn{},
		PrepareBoot:        &recordingOperationBootPreparation{},
		HostPhase:          &recordingOperationHostPhase{},
		DriverCapabilities: observer,
	}

	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(operation),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if observer.calls != 1 {
		t.Fatalf("ObserveAndPersist() calls = %d, want 1", observer.calls)
	}
	if observer.driver != driverdomain.WoL {
		t.Fatalf("driver = %q, want %q", observer.driver, driverdomain.WoL)
	}
}

func TestTartHostOperationReconcilerはPowerOn前にPowerStateを観測する(t *testing.T) {
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
	observer := &recordingOperationDriverPowerState{}
	reconciler := &TartHostOperationReconciler{
		Client:             k8sClient,
		Scheme:             scheme,
		PowerOn:            successfulOperationPowerOn{},
		PrepareBoot:        &recordingOperationBootPreparation{},
		HostPhase:          &recordingOperationHostPhase{},
		DriverCapabilities: &recordingOperationDriverCapabilities{},
		DriverPowerState:   observer,
	}

	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(operation),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if observer.calls != 1 {
		t.Fatalf("ObserveAndPersist() calls = %d, want 1", observer.calls)
	}
	if observer.driver != driverdomain.WoL {
		t.Fatalf("driver = %q, want %q", observer.driver, driverdomain.WoL)
	}
}

func TestTartHostOperationReconcilerはPowerOn前にBootStateを観測する(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := operationTestHost()
	host.Spec.Management.PowerDriver = redfishDriverName
	host.Spec.Management.BootDriver = redfishDriverName
	host.Spec.Management.Redfish = &infrastructurev1beta1.RedfishManagement{
		Endpoint: "https://bmc.example.test",
	}
	operation := operationTestUpdate(host)
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(host, operation).
		Build()
	observer := &recordingOperationDriverBootState{}
	reconciler := &TartHostOperationReconciler{
		Client:             k8sClient,
		Scheme:             scheme,
		PowerOn:            successfulOperationPowerOn{},
		PrepareBoot:        &recordingOperationBootPreparation{},
		HostPhase:          &recordingOperationHostPhase{},
		DriverCapabilities: &recordingOperationDriverCapabilities{},
		DriverPowerState:   &recordingOperationDriverPowerState{},
		DriverBootState:    observer,
	}

	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(operation),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if observer.calls != 1 {
		t.Fatalf("ObserveAndPersist() calls = %d, want 1", observer.calls)
	}
	if observer.driver != driverdomain.Redfish {
		t.Fatalf("driver = %q, want %q", observer.driver, driverdomain.Redfish)
	}
}

func TestTartHostOperationReconcilerはPreparingBootでPowerOnしてWaitingForAgentへ進める(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := operationTestHost()
	host.Spec.Management.PowerDriver = redfishDriverName
	host.Spec.Management.BootDriver = redfishDriverName
	host.Spec.Management.Redfish = &infrastructurev1beta1.RedfishManagement{
		Endpoint: "https://bmc.example.test",
	}
	operation := operationTestUpdate(host)
	operation.Status.Phase = infrastructurev1beta1.TartHostOperationPhasePreparingBoot
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(host, operation).
		Build()
	observer := &recordingOperationDriverBootState{}
	reconciler := &TartHostOperationReconciler{
		Client:          k8sClient,
		Scheme:          scheme,
		PowerOn:         successfulOperationPowerOn{},
		HostPhase:       &recordingOperationHostPhase{},
		DriverBootState: observer,
	}

	result, err := reconciler.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(operation),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("RequeueAfter = %s, want 0", result.RequeueAfter)
	}
	if observer.calls != 1 {
		t.Fatalf("ObserveBootAndPersist() calls = %d, want 1", observer.calls)
	}
	current := &infrastructurev1beta1.TartHostOperation{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(operation), current); err != nil {
		t.Fatalf("get TartHostOperation: %v", err)
	}
	if current.Status.Phase != infrastructurev1beta1.TartHostOperationPhaseWaitingForAgent {
		t.Fatalf("phase = %q, want WaitingForAgent", current.Status.Phase)
	}
}

func TestTartHostOperationReconcilerはRedfish再起動後の再観測でStatusを上書きする(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := operationTestHost()
	host.Generation = 7
	host.Spec.Management.PowerDriver = redfishDriverName
	host.Spec.Management.BootDriver = redfishDriverName
	host.Spec.Management.Redfish = &infrastructurev1beta1.RedfishManagement{
		Endpoint: "https://bmc.example.test",
	}
	host.Status.PowerState = infrastructurev1beta1.PowerStateOff
	host.Status.BootState = &infrastructurev1beta1.BootStateStatus{
		OverrideEnabled: false,
		OverrideTarget:  infrastructurev1beta1.BootTargetPXE,
		VirtualMedia: infrastructurev1beta1.VirtualMediaStatus{
			Inserted:    false,
			Image:       "https://stale.example.test/old.iso",
			OperationID: "stale-operation",
		},
	}
	operation := operationTestUpdate(host)
	operation.Status.Phase = infrastructurev1beta1.TartHostOperationPhasePreparingBoot
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHost{}, &infrastructurev1beta1.TartHostOperation{}).
		WithObjects(host, operation).
		Build()
	powerObserver := &recordingOperationPowerStateObserver{
		state: driverdomain.PowerStateOn,
	}
	bootObserver := &recordingOperationBootStateObserver{
		state: driverdomain.BootState{
			OverrideEnabled: true,
			OverrideTarget:  driverdomain.BootTargetVirtualMedia,
			MediaInserted:   true,
			MediaImage:      "https://controller.example.test/agent.iso",
			MediaOperation:  "f4353748-c9ea-41c6-b321-94197b64330e",
		},
	}
	writer := v1beta1hostadapter.NewService(k8sClient)
	reconciler := &TartHostOperationReconciler{
		Client:           k8sClient,
		Scheme:           scheme,
		PowerOn:          successfulOperationPowerOn{},
		HostPhase:        &recordingOperationHostPhase{},
		DriverPowerState: driverstateadapter.NewService(powerObserver, nil, writer),
		DriverBootState:  driverstateadapter.NewService(nil, bootObserver, writer),
	}

	result, err := reconciler.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(operation),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.RequeueAfter != 0 {
		t.Fatalf("RequeueAfter = %s, want 0", result.RequeueAfter)
	}

	current := &infrastructurev1beta1.TartHost{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(host), current); err != nil {
		t.Fatalf("get TartHost: %v", err)
	}
	if current.Status.PowerState != infrastructurev1beta1.PowerStateOn {
		t.Fatalf("status.powerState = %q, want On", current.Status.PowerState)
	}
	if current.Status.BootState == nil {
		t.Fatal("status.bootState = nil, want observed state")
	}
	if !current.Status.BootState.OverrideEnabled ||
		current.Status.BootState.OverrideTarget != infrastructurev1beta1.BootTargetVirtualMedia {
		t.Fatalf("status.bootState = %#v, want VirtualMedia override", current.Status.BootState)
	}
	if !current.Status.BootState.VirtualMedia.Inserted ||
		current.Status.BootState.VirtualMedia.Image != "https://controller.example.test/agent.iso" ||
		current.Status.BootState.VirtualMedia.OperationID != "f4353748-c9ea-41c6-b321-94197b64330e" {
		t.Fatalf("status.bootState.virtualMedia = %#v, want mounted media", current.Status.BootState.VirtualMedia)
	}
	if current.Status.ObservedGeneration != current.Generation {
		t.Fatalf("observedGeneration = %d, want %d", current.Status.ObservedGeneration, current.Generation)
	}
	if powerObserver.calls != 1 {
		t.Fatalf("power observer calls = %d, want 1", powerObserver.calls)
	}
	if powerObserver.driver != driverdomain.Redfish {
		t.Fatalf("power observer driver = %q, want %q", powerObserver.driver, driverdomain.Redfish)
	}
	if bootObserver.calls != 1 {
		t.Fatalf("boot observer calls = %d, want 1", bootObserver.calls)
	}
	if bootObserver.driver != driverdomain.Redfish {
		t.Fatalf("boot observer driver = %q, want %q", bootObserver.driver, driverdomain.Redfish)
	}
}

func TestTartHostOperationReconcilerはRedfishPreferredBootTransportをPrepareBootへ渡す(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := operationTestHost()
	host.Spec.Management.PowerDriver = redfishDriverName
	host.Spec.Management.BootDriver = redfishDriverName
	host.Spec.Management.Redfish = &infrastructurev1beta1.RedfishManagement{
		Endpoint:               "https://bmc.example.test",
		PreferredBootTransport: infrastructurev1beta1.BootTransportRedfishPXE,
	}
	operation := operationTestUpdate(host)
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(host, operation).
		Build()
	preparation := &recordingOperationBootPreparation{}
	reconciler := &TartHostOperationReconciler{
		Client:             k8sClient,
		Scheme:             scheme,
		PowerOn:            successfulOperationPowerOn{},
		PrepareBoot:        preparation,
		HostPhase:          &recordingOperationHostPhase{},
		DriverCapabilities: &recordingOperationDriverCapabilities{},
	}

	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(operation),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if preparation.calls != 1 {
		t.Fatalf("PrepareBoot() calls = %d, want 1", preparation.calls)
	}
	if preparation.preferred == nil || *preparation.preferred != driverdomain.BootTargetPXE {
		t.Fatalf("preferred = %v, want PXE", preparation.preferred)
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
	degraded := apimeta.FindStatusCondition(current.Status.Conditions, appupdate.ConditionDegraded)
	if degraded == nil || degraded.Reason != "HealthCheckFailed" {
		t.Fatalf("Degraded condition = %#v", degraded)
	}
}

func TestTartHostOperationReconcilerはBootReport未着をBoot失敗試行として数える(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := operationTestHost()
	operation := operationTestUpdate(host)
	operation.Spec.Deadline = metav1.NewTime(time.Now().Add(-time.Minute))
	operation.Status.Phase = infrastructurev1beta1.TartHostOperationPhaseBootTrial
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

	for attempt := int32(1); attempt <= 3; attempt++ {
		_, err := reconciler.Reconcile(t.Context(), ctrl.Request{
			NamespacedName: client.ObjectKeyFromObject(operation),
		})
		if err != nil {
			t.Fatalf("attempt %d Reconcile() error = %v", attempt, err)
		}
		current := &infrastructurev1beta1.TartHostOperation{}
		if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(operation), current); err != nil {
			t.Fatalf("get TartHostOperation: %v", err)
		}
		if current.Status.Attempt != attempt {
			t.Fatalf("attempt %d status.attempt = %d", attempt, current.Status.Attempt)
		}
		if attempt < 3 {
			if current.Status.Phase != infrastructurev1beta1.TartHostOperationPhaseBootTrial {
				t.Fatalf("attempt %d phase = %q, want BootTrial", attempt, current.Status.Phase)
			}
			continue
		}
		if current.Status.Phase != infrastructurev1beta1.TartHostOperationPhaseRollingBack {
			t.Fatalf("attempt %d phase = %q, want RollingBack", attempt, current.Status.Phase)
		}
		degraded := apimeta.FindStatusCondition(current.Status.Conditions, appupdate.ConditionDegraded)
		if degraded == nil || degraded.Reason != "BootFailed" {
			t.Fatalf("attempt %d Degraded condition = %#v", attempt, degraded)
		}
	}
}

func TestTartHostOperationReconcilerはClean開始時にHostをCleaningへ移す(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := operationTestHost()
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-clean-start",
			Namespace: "default",
			UID:       types.UID("machine-clean-start-uid"),
		},
		Spec: infrastructurev1beta1.TartMachineSpec{
			DeletionPolicy: infrastructurev1beta1.DeletionPolicyRetainData,
		},
	}
	operation := operationTestUpdate(host)
	operation.Spec.Type = infrastructurev1beta1.OperationTypeClean
	operation.Spec.MachineRef = &infrastructurev1beta1.ResourceReference{
		Namespace: machine.Namespace,
		Name:      machine.Name,
		UID:       machine.UID,
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(host, machine, operation).
		Build()
	hostPhase := &recordingOperationHostPhase{}
	reconciler := &TartHostOperationReconciler{
		Client:             k8sClient,
		Scheme:             scheme,
		PowerOn:            successfulOperationPowerOn{},
		PrepareBoot:        &recordingOperationBootPreparation{},
		HostPhase:          hostPhase,
		DriverCapabilities: &recordingOperationDriverCapabilities{},
	}

	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(operation),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !hostPhase.cleaning {
		t.Fatal("MarkHostCleaningForDeletion() was not called")
	}
	if hostPhase.provisioning || hostPhase.updating {
		t.Fatalf("unexpected host phase calls = %#v", hostPhase)
	}
}

func TestTartHostOperationReconcilerはCleanPolicy不正をConditionとEventへ反映する(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := operationTestHost()
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-clean-invalid",
			Namespace: "default",
			UID:       types.UID("machine-clean-invalid-uid"),
		},
		Spec: infrastructurev1beta1.TartMachineSpec{
			DeletionPolicy: infrastructurev1beta1.DeletionPolicyWipeAll,
		},
	}
	operation := operationTestUpdate(host)
	operation.Spec.Type = infrastructurev1beta1.OperationTypeClean
	operation.Spec.MachineRef = &infrastructurev1beta1.ResourceReference{
		Namespace: machine.Namespace,
		Name:      machine.Name,
		UID:       machine.UID,
	}
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(host, machine, operation).
		Build()
	hostPhase := &recordingOperationHostPhase{}
	recorder := events.NewFakeRecorder(1)
	reconciler := &TartHostOperationReconciler{
		Client:    k8sClient,
		Scheme:    scheme,
		PowerOn:   successfulOperationPowerOn{},
		HostPhase: hostPhase,
		Recorder:  recorder,
	}

	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(operation),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if hostPhase.cleaning {
		t.Fatal("MarkHostCleaningForDeletion() was called for rejected Operation")
	}
	current := &infrastructurev1beta1.TartHostOperation{}
	if err := k8sClient.Get(t.Context(), client.ObjectKeyFromObject(operation), current); err != nil {
		t.Fatalf("get TartHostOperation: %v", err)
	}
	degraded := apimeta.FindStatusCondition(current.Status.Conditions, appupdate.ConditionDegraded)
	if degraded == nil || degraded.Reason != "CleaningPolicyRequired" {
		t.Fatalf("Degraded condition = %#v", degraded)
	}
	select {
	case event := <-recorder.Events:
		if !strings.Contains(event, "Warning CleaningPolicyRequired") {
			t.Fatalf("event = %q, want Warning CleaningPolicyRequired", event)
		}
	case <-time.After(time.Second):
		t.Fatal("event was not recorded")
	}
}

func TestTartHostOperationReconcilerは手動WipeAll開始時にAvailableHostをCleaningへ移す(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := operationTestHost()
	host.Status.Phase = infrastructurev1beta1.TartHostPhaseAvailable
	host.Status.LastStablePhase = infrastructurev1beta1.TartHostPhaseAvailable
	operation := operationTestUpdate(host)
	operation.Spec.Type = infrastructurev1beta1.OperationTypeWipeAll
	operation.Spec.MachineRef = nil
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(host, operation).
		Build()
	hostPhase := &recordingOperationHostPhase{}
	reconciler := &TartHostOperationReconciler{
		Client:             k8sClient,
		Scheme:             scheme,
		PowerOn:            successfulOperationPowerOn{},
		PrepareBoot:        &recordingOperationBootPreparation{},
		HostPhase:          hostPhase,
		DriverCapabilities: &recordingOperationDriverCapabilities{},
	}

	_, err := reconciler.Reconcile(t.Context(), ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(operation),
	})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !hostPhase.cleaning {
		t.Fatal("MarkHostCleaningForDeletion() was not called")
	}
	if hostPhase.provisioning || hostPhase.updating {
		t.Fatalf("unexpected host phase calls = %#v", hostPhase)
	}
}

func TestTartHostOperationReconcilerは手動WipeAll完了時にHostをAvailableへ戻す(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := operationTestHost()
	host.Status.Phase = infrastructurev1beta1.TartHostPhaseCleaning
	host.Status.LastStablePhase = infrastructurev1beta1.TartHostPhaseAvailable
	operation := operationTestUpdate(host)
	operation.Spec.Type = infrastructurev1beta1.OperationTypeWipeAll
	operation.Spec.MachineRef = nil
	operation.Status.Phase = infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth
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
	if !hostPhase.available {
		t.Fatal("MarkHostAvailable() was not called")
	}
}

func TestTartHostOperationReconcilerはRetainData完了時にHostをRetainedへ移す(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := operationTestHost()
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-clean",
			Namespace: "default",
			UID:       types.UID("machine-clean-uid"),
		},
		Spec: infrastructurev1beta1.TartMachineSpec{
			DeletionPolicy: infrastructurev1beta1.DeletionPolicyRetainData,
		},
	}
	operation := operationTestUpdate(host)
	operation.Spec.Type = infrastructurev1beta1.OperationTypeClean
	operation.Spec.MachineRef = &infrastructurev1beta1.ResourceReference{
		Namespace: machine.Namespace,
		Name:      machine.Name,
		UID:       machine.UID,
	}
	operation.Status.Phase = infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(host, machine, operation).
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
	if !hostPhase.retained {
		t.Fatal("MarkHostRetained() was not called")
	}
}

func TestTartHostOperationReconcilerはRetainState完了時にHostをDetachedへ移す(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := infrastructurev1beta1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}
	host := operationTestHost()
	machine := &infrastructurev1beta1.TartMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-detach",
			Namespace: "default",
			UID:       types.UID("machine-detach-uid"),
		},
		Spec: infrastructurev1beta1.TartMachineSpec{
			DeletionPolicy: infrastructurev1beta1.DeletionPolicyRetainState,
		},
	}
	operation := operationTestUpdate(host)
	operation.Spec.Type = infrastructurev1beta1.OperationTypeClean
	operation.Spec.MachineRef = &infrastructurev1beta1.ResourceReference{
		Namespace: machine.Namespace,
		Name:      machine.Name,
		UID:       machine.UID,
	}
	operation.Status.Phase = infrastructurev1beta1.TartHostOperationPhaseAwaitingHealth
	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&infrastructurev1beta1.TartHostOperation{}).
		WithObjects(host, machine, operation).
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
	if !hostPhase.detached {
		t.Fatal("MarkHostDetached() was not called")
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

type phaseRecordingOperationPowerOn struct {
	client        client.Client
	key           client.ObjectKey
	calls         int
	observedPhase infrastructurev1beta1.TartHostOperationPhase
}

func (powerOn *phaseRecordingOperationPowerOn) PowerOn(
	ctx context.Context,
	_ driverdomain.Name,
	_ driverdomain.HostTarget,
	_ operationdomain.ID,
	_ applicationdriver.Invocation,
) error {
	powerOn.calls++
	operation := &infrastructurev1beta1.TartHostOperation{}
	if err := powerOn.client.Get(ctx, powerOn.key, operation); err != nil {
		return err
	}
	powerOn.observedPhase = operation.Status.Phase
	return nil
}

type recordingOperationHostPhase struct {
	provisioning bool
	updating     bool
	provisioned  bool
	recovery     bool
	cleaning     bool
	available    bool
	retained     bool
	detached     bool
}

type recordingOperationDriverCapabilities struct {
	calls  int
	driver driverdomain.Name
}

type recordingOperationDriverPowerState struct {
	calls  int
	driver driverdomain.Name
}

type recordingOperationDriverBootState struct {
	calls  int
	driver driverdomain.Name
}

type recordingOperationPowerStateObserver struct {
	calls  int
	driver driverdomain.Name
	state  driverdomain.PowerState
}

type recordingOperationBootStateObserver struct {
	calls  int
	driver driverdomain.Name
	state  driverdomain.BootState
}

type recordingOperationBootPreparation struct {
	calls     int
	preferred *driverdomain.BootTarget
}

func (observer *recordingOperationDriverCapabilities) ObserveAndPersist(
	_ context.Context,
	driver driverdomain.Name,
	_ driverdomain.HostTarget,
	_ *infrastructurev1beta1.TartHost,
	_ applicationdriver.Invocation,
) error {
	observer.calls++
	observer.driver = driver
	_, _ = capabilitydomain.NewSet(capabilitydomain.PowerOn)
	return nil
}

func (observer *recordingOperationDriverPowerState) ObserveAndPersist(
	_ context.Context,
	driver driverdomain.Name,
	_ driverdomain.HostTarget,
	_ *infrastructurev1beta1.TartHost,
	_ applicationdriver.Invocation,
) error {
	observer.calls++
	observer.driver = driver
	return nil
}

func (observer *recordingOperationDriverBootState) ObserveBootAndPersist(
	_ context.Context,
	driver driverdomain.Name,
	_ driverdomain.HostTarget,
	_ *infrastructurev1beta1.TartHost,
	_ applicationdriver.Invocation,
) error {
	observer.calls++
	observer.driver = driver
	return nil
}

func (observer *recordingOperationPowerStateObserver) ObservePowerState(
	_ context.Context,
	driver driverdomain.Name,
	_ driverdomain.HostTarget,
	_ applicationdriver.Invocation,
) (driverdomain.PowerState, error) {
	observer.calls++
	observer.driver = driver
	return observer.state, nil
}

func (observer *recordingOperationBootStateObserver) ObserveBootState(
	_ context.Context,
	driver driverdomain.Name,
	_ driverdomain.HostTarget,
	_ applicationdriver.Invocation,
) (driverdomain.BootState, error) {
	observer.calls++
	observer.driver = driver
	return observer.state, nil
}

func (preparation *recordingOperationBootPreparation) PrepareBoot(
	_ context.Context,
	_ driverdomain.Name,
	_ driverdomain.HostTarget,
	_ operationdomain.ID,
	preferred *driverdomain.BootTarget,
	_ applicationdriver.Invocation,
) (driverdomain.BootTarget, error) {
	preparation.calls++
	preparation.preferred = preferred
	if preferred != nil {
		return *preferred, nil
	}
	return driverdomain.BootTargetHTTP, nil
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

func (phase *recordingOperationHostPhase) MarkHostAvailable(
	context.Context,
	*infrastructurev1beta1.TartHost,
) error {
	phase.available = true
	return nil
}

func (phase *recordingOperationHostPhase) MarkHostCleaningForDeletion(
	context.Context,
	*infrastructurev1beta1.TartHost,
	infrastructurev1beta1.DeletionPolicy,
) error {
	phase.cleaning = true
	return nil
}

func (phase *recordingOperationHostPhase) MarkHostRetained(
	context.Context,
	*infrastructurev1beta1.TartHost,
) error {
	phase.retained = true
	return nil
}

func (phase *recordingOperationHostPhase) MarkHostDetached(
	context.Context,
	*infrastructurev1beta1.TartHost,
) error {
	phase.detached = true
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
			TargetSlot:  infrastructurev1beta1.OSSlotB,
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
