//go:build wireinject

package wire

import (
	"github.com/google/wire"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterlifecycle "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster/workflow/reconcile_cluster"
	clusterstatus "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster/workflow/reconcile_cluster_status"
	machinetemplatelifecycle "github.com/walnuts1018/cluster-api-provider-tart/domain/cluster/workflow/reconcile_machine_template"
	machinedeletion "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/delete_machine"
	operationexecution "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/execute_operation"
	machineexecution "github.com/walnuts1018/cluster-api-provider-tart/domain/provisioning/workflow/reconcile_machine"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/driver"
	"github.com/walnuts1018/cluster-api-provider-tart/infrastructure/k8s_controller"
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

func InitializeReconcilers(k8sClient client.Client, scheme *runtime.Scheme) (Reconcilers, error) {
	wire.Build(
		k8sdrivercapability.NewService,
		k8sdriverstate.NewService,
		k8sdrivertarget.NewService,
		k8sallocation.NewService,
		k8smachinehealth.NewObserver,
		k8sv1beta1host.NewService,

		provideInitialProvisioningStep,
		wire.Bind(new(machineexecution.HostReferenceService), new(*k8sallocation.Service)),
		wire.Bind(new(machineexecution.NodeHealthObserver), new(*k8smachinehealth.Observer)),
		wire.Bind(new(operationexecution.PowerOnService), new(*applicationdriver.Service)),
		wire.Bind(new(operationexecution.BootPreparationService), new(*applicationdriver.Service)),
		wire.Bind(new(operationexecution.HostPhaseService), new(*k8sv1beta1host.Service)),
		wire.Bind(new(operationexecution.DriverTargetBuilder), new(*k8sdrivertarget.Service)),
		wire.Bind(new(operationexecution.DriverCapabilityObserver), new(*k8sdrivercapability.Service)),
		wire.Bind(new(operationexecution.DriverPowerStateObserver), new(*k8sdriverstate.Service)),
		wire.Bind(new(operationexecution.DriverBootStateObserver), new(*k8sdriverstate.Service)),
		wire.Bind(new(k8sdrivercapability.CapabilityDiscoverer), new(*applicationdriver.Service)),
		wire.Bind(new(k8sdrivercapability.HostCapabilityWriter), new(*k8sv1beta1host.Service)),
		wire.Bind(new(k8sdriverstate.PowerStateObserver), new(*applicationdriver.Service)),
		wire.Bind(new(k8sdriverstate.BootStateObserver), new(*applicationdriver.Service)),
		wire.Bind(new(k8sdriverstate.HostPowerStateWriter), new(*k8sv1beta1host.Service)),

		woladapter.Default,
		redfishadapter.New,
		provideDriverRegistry,
		applicationdriver.NewService,
		provideTartClusterReconciler,
		provideTartMachineTemplateReconciler,
		provideTartMachineV1Beta1Reconciler,
		provideCleaningStep,
		provideTartHostOperationReconciler,
		provideReconcilers,
	)
	return Reconcilers{}, nil
}
