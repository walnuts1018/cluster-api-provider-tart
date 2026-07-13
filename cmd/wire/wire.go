//go:build wireinject

package wire

import (
	"github.com/google/wire"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redfishadapter "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/driver/redfish"
	woladapter "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/driver/wol"
	k8sallocation "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/allocation"
	k8sdrivercapability "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/drivercapability"
	k8sdriverstate "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/driverstate"
	k8sdrivertarget "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/drivertarget"
	k8smachinehealth "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/machinehealth"
	k8soperation "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/operation"
	k8sv1beta1host "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/v1beta1host"
	clusterlifecycle "github.com/walnuts1018/cluster-api-provider-tart/internal/application/clusterlifecycle"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/internal/application/driver"
	appprovisioning "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning"
	machinedeletion "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinedeletion"
	machineexecution "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machineexecution"
	machinelifecycle "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinelifecycle"
	machinetemplatelifecycle "github.com/walnuts1018/cluster-api-provider-tart/internal/application/machinetemplatelifecycle"
	operationexecution "github.com/walnuts1018/cluster-api-provider-tart/internal/application/operationexecution"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/controller"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
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

func provideTartClusterReconciler(k8sClient client.Client, scheme *runtime.Scheme) *controller.TartClusterReconciler {
	return &controller.TartClusterReconciler{
		Client:    k8sClient,
		Scheme:    scheme,
		Lifecycle: clusterlifecycle.NewWorkflow(k8sClient),
	}
}

func provideTartMachineTemplateReconciler(k8sClient client.Client, scheme *runtime.Scheme) *controller.TartMachineTemplateReconciler {
	return &controller.TartMachineTemplateReconciler{
		Client:    k8sClient,
		Scheme:    scheme,
		Lifecycle: machinetemplatelifecycle.NewWorkflow(k8sClient),
	}
}

func provideTartMachineV1Beta1Reconciler(
	k8sClient client.Client,
	hostReferences machineexecution.HostReferenceService,
	nodeHealth machineexecution.NodeHealthObserver,
	provisioner machineexecution.ProvisionWorkflow,
	cleaner machinedeletion.CleaningWorkflow,
) *controller.TartMachineV1Beta1Reconciler {
	return &controller.TartMachineV1Beta1Reconciler{
		Client:         k8sClient,
		Lifecycle:      machinelifecycle.NewWorkflow(k8sClient, hostReferences, nodeHealth, provisioner, cleaner, nil),
		HostReferences: hostReferences,
		NodeHealth:     nodeHealth,
		Provisioner:    provisioner,
		Cleaner:        cleaner,
	}
}

func provideCleaningWorkflow() machinedeletion.CleaningWorkflow {
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
		k8soperation.NewService,
		k8sv1beta1host.NewService,

		appprovisioning.NewWorkflow,
		wire.Bind(new(machineexecution.HostReferenceService), new(*k8sallocation.Service)),
		wire.Bind(new(machineexecution.NodeHealthObserver), new(*k8smachinehealth.Observer)),
		wire.Bind(new(machineexecution.ProvisionWorkflow), new(*appprovisioning.Workflow)),
		wire.Bind(new(appprovisioning.HostReserveService), new(*k8sallocation.Service)),
		wire.Bind(new(appprovisioning.HostPhaseService), new(*k8sv1beta1host.Service)),
		wire.Bind(new(appprovisioning.OperationService), new(*k8soperation.Service)),
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
		provideCleaningWorkflow,
		provideTartHostOperationReconciler,
		provideReconcilers,
	)
	return Reconcilers{}, nil
}
