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

//go:generate go tool kessoku $GOFILE

package kessoku

import (
	kessokulib "github.com/mazrean/kessoku"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterlifecycle "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster/workflow/reconcile_cluster"
	clusterstatus "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster/workflow/reconcile_cluster_status"
	machinetemplatelifecycle "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster/workflow/reconcile_machine_template"
	machinedeletion "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/delete_machine"
	operationexecution "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/execute_operation"
	machineexecution "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/reconcile_machine"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/driver"
	controller "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/k8s_controller"
	k8sallocation "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/repository/k8s/allocation"
	k8sdrivercapability "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/repository/k8s/drivercapability"
	k8sdriverstate "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/repository/k8s/driverstate"
	k8sdrivertarget "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/repository/k8s/drivertarget"
	k8smachinehealth "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/repository/k8s/machinehealth"
	k8sv1beta1host "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/repository/k8s/v1beta1host"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/driver"
	redfishadapter "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/driver/redfish"
	woladapter "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/driver/wol"
	resourcefinalizer "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/resource_finalizer"
)

type Reconcilers struct {
	TartCluster         *controller.TartClusterReconciler
	TartMachineTemplate *controller.TartMachineTemplateReconciler
	TartMachineV1Beta1  *controller.TartMachineV1Beta1Reconciler
	TartHostOperation   *controller.TartHostOperationReconciler
	Driver              *applicationdriver.Service
}

func provideDriverRegistry(
	wolDriver *woladapter.Adapter,
	redfishDriver *redfishadapter.Adapter,
) (*applicationdriver.Registry, error) {
	registry := applicationdriver.NewRegistry()
	if err := registry.RegisterPowerOn(driverdomain.WoL, wolDriver); err != nil {
		return nil, err
	}
	if err := registry.RegisterPowerOn(driverdomain.Redfish, redfishDriver); err != nil {
		return nil, err
	}
	if err := registry.RegisterPowerOff(driverdomain.Redfish, redfishDriver); err != nil {
		return nil, err
	}
	if err := registry.RegisterPowerStateObserver(driverdomain.Redfish, redfishDriver); err != nil {
		return nil, err
	}
	if err := registry.RegisterBootOverride(driverdomain.Redfish, redfishDriver); err != nil {
		return nil, err
	}
	if err := registry.RegisterBootStateObserver(driverdomain.Redfish, redfishDriver); err != nil {
		return nil, err
	}
	if err := registry.RegisterVirtualMedia(driverdomain.Redfish, redfishDriver); err != nil {
		return nil, err
	}
	if err := registry.RegisterCapabilityDiscoverer(driverdomain.Redfish, redfishDriver); err != nil {
		return nil, err
	}
	return registry, nil
}

func provideInitialProvisioningStep() machineexecution.Provisioner {
	return nil
}

func provideTartClusterReconciler(k8sClient client.Client, scheme *runtime.Scheme) *controller.TartClusterReconciler {
	return &controller.TartClusterReconciler{
		Client: k8sClient,
		Scheme: scheme,
		Lifecycle: clusterlifecycle.NewWorkflowWithPorts(
			resourcefinalizer.NewTartClusterService(k8sClient),
			clusterstatus.NewWorkflow(k8sClient),
		),
	}
}

func provideTartMachineTemplateReconciler(k8sClient client.Client, scheme *runtime.Scheme) *controller.TartMachineTemplateReconciler {
	return &controller.TartMachineTemplateReconciler{
		Client: k8sClient,
		Scheme: scheme,
		Lifecycle: machinetemplatelifecycle.NewWorkflowWithFinalizer(
			resourcefinalizer.NewTartMachineTemplateService(k8sClient),
		),
	}
}

func provideTartMachineV1Beta1Reconciler(
	k8sClient client.Client,
	hostReferences machineexecution.HostReferenceService,
	nodeHealth machineexecution.NodeHealthObserver,
	provisioner machineexecution.Provisioner,
	cleaner machinedeletion.CleaningWorkflow,
) *controller.TartMachineV1Beta1Reconciler {
	return &controller.TartMachineV1Beta1Reconciler{
		Client:         k8sClient,
		HostReferences: hostReferences,
		NodeHealth:     nodeHealth,
		Provisioner:    provisioner,
		Cleaner:        cleaner,
	}
}

func provideCleaningStep() machinedeletion.CleaningWorkflow {
	return nil
}

func provideTartHostOperationReconciler(
	k8sClient client.Client,
	scheme *runtime.Scheme,
	powerOn operationexecution.PowerOnService,
	prepareBoot operationexecution.BootPreparationService,
	hostPhase operationexecution.HostPhaseService,
	targets operationexecution.DriverTargetBuilder,
	driverCapabilities operationexecution.DriverCapabilityObserver,
	driverPowerState operationexecution.DriverPowerStateObserver,
	driverBootState operationexecution.DriverBootStateObserver,
) *controller.TartHostOperationReconciler {
	return &controller.TartHostOperationReconciler{
		Client:             k8sClient,
		Scheme:             scheme,
		PowerOn:            powerOn,
		PrepareBoot:        prepareBoot,
		HostPhase:          hostPhase,
		Targets:            targets,
		DriverCapabilities: driverCapabilities,
		DriverPowerState:   driverPowerState,
		DriverBootState:    driverBootState,
	}
}

func provideReconcilers(
	tartCluster *controller.TartClusterReconciler,
	tartMachineTemplate *controller.TartMachineTemplateReconciler,
	tartMachineV1Beta1 *controller.TartMachineV1Beta1Reconciler,
	tartHostOperation *controller.TartHostOperationReconciler,
	driverService *applicationdriver.Service,
) Reconcilers {
	return Reconcilers{
		TartCluster:         tartCluster,
		TartMachineTemplate: tartMachineTemplate,
		TartMachineV1Beta1:  tartMachineV1Beta1,
		TartHostOperation:   tartHostOperation,
		Driver:              driverService,
	}
}

func provideHostReference(service *k8sallocation.Service) machineexecution.HostReferenceService {
	return service
}

func provideNodeHealth(service *k8smachinehealth.Observer) machineexecution.NodeHealthObserver {
	return service
}

func providePowerOn(service *applicationdriver.Service) operationexecution.PowerOnService {
	return service
}

func provideBootPreparation(service *applicationdriver.Service) operationexecution.BootPreparationService {
	return service
}

func provideHostPhase(service *k8sv1beta1host.Service) operationexecution.HostPhaseService {
	return service
}

func provideDriverTarget(service *k8sdrivertarget.Service) operationexecution.DriverTargetBuilder {
	return service
}

func provideDriverCapability(service *k8sdrivercapability.Service) operationexecution.DriverCapabilityObserver {
	return service
}

func provideDriverPowerState(service *k8sdriverstate.Service) operationexecution.DriverPowerStateObserver {
	return service
}

func provideDriverBootState(service *k8sdriverstate.Service) operationexecution.DriverBootStateObserver {
	return service
}

func provideCapabilityDiscoverer(service *applicationdriver.Service) k8sdrivercapability.CapabilityDiscoverer {
	return service
}

func provideHostCapabilityWriter(service *k8sv1beta1host.Service) k8sdrivercapability.HostCapabilityWriter {
	return service
}

func providePowerStateObserver(service *applicationdriver.Service) k8sdriverstate.PowerStateObserver {
	return service
}

func provideBootStateObserver(service *applicationdriver.Service) k8sdriverstate.BootStateObserver {
	return service
}

func provideHostPowerStateWriter(service *k8sv1beta1host.Service) k8sdriverstate.HostPowerStateWriter {
	return service
}

var _ = kessokulib.Inject[Reconcilers](
	"InitializeReconcilers",
	kessokulib.Provide(k8sdrivercapability.NewService),
	kessokulib.Provide(k8sdriverstate.NewService),
	kessokulib.Provide(k8sdrivertarget.NewService),
	kessokulib.Provide(k8sallocation.NewService),
	kessokulib.Provide(k8smachinehealth.NewObserver),
	kessokulib.Provide(k8sv1beta1host.NewService),
	kessokulib.Provide(provideInitialProvisioningStep),
	kessokulib.Provide(provideHostReference),
	kessokulib.Provide(provideNodeHealth),
	kessokulib.Provide(providePowerOn),
	kessokulib.Provide(provideBootPreparation),
	kessokulib.Provide(provideHostPhase),
	kessokulib.Provide(provideDriverTarget),
	kessokulib.Provide(provideDriverCapability),
	kessokulib.Provide(provideDriverPowerState),
	kessokulib.Provide(provideDriverBootState),
	kessokulib.Provide(provideCapabilityDiscoverer),
	kessokulib.Provide(provideHostCapabilityWriter),
	kessokulib.Provide(providePowerStateObserver),
	kessokulib.Provide(provideBootStateObserver),
	kessokulib.Provide(provideHostPowerStateWriter),
	kessokulib.Provide(woladapter.Default),
	kessokulib.Provide(redfishadapter.New),
	kessokulib.Provide(provideDriverRegistry),
	kessokulib.Provide(applicationdriver.NewService),
	kessokulib.Provide(provideTartClusterReconciler),
	kessokulib.Provide(provideTartMachineTemplateReconciler),
	kessokulib.Provide(provideTartMachineV1Beta1Reconciler),
	kessokulib.Provide(provideCleaningStep),
	kessokulib.Provide(provideTartHostOperationReconciler),
	kessokulib.Provide(provideReconcilers),
)
