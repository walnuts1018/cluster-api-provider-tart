//go:build wireinject

package wire

import (
	"github.com/google/wire"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	redfishadapter "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/driver/redfish"
	woladapter "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/driver/wol"
	k8sallocation "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/allocation"
	k8sbootstraptoken "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/bootstraptoken"
	k8sdrivercapability "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/drivercapability"
	k8sdriverstate "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/driverstate"
	k8sdrivertarget "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/drivertarget"
	k8shost "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/host"
	k8smachinehealth "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/machinehealth"
	k8soperation "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/operation"
	k8sv1beta1host "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/v1beta1host"
	applicationbootstraptoken "github.com/walnuts1018/cluster-api-provider-tart/internal/application/bootstraptoken"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/internal/application/driver"
	applicationhost "github.com/walnuts1018/cluster-api-provider-tart/internal/application/host"
	appprovisioning "github.com/walnuts1018/cluster-api-provider-tart/internal/application/initialprovisioning"
	applicationprovisioning "github.com/walnuts1018/cluster-api-provider-tart/internal/application/provisioning"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/controller"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
)

type Reconcilers struct {
	TartHost            *controller.TartHostReconciler
	TartMachine         *controller.TartMachineReconciler
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

func provideTartHostReconciler(k8sClient client.Client, scheme *runtime.Scheme, hostService applicationhost.Service) *controller.TartHostReconciler {
	return &controller.TartHostReconciler{
		Client:      k8sClient,
		Scheme:      scheme,
		HostService: hostService,
	}
}

func provideTartMachineReconciler(
	k8sClient client.Client,
	scheme *runtime.Scheme,
	hostService applicationhost.Service,
	tokenService applicationbootstraptoken.Service,
	provisioningService applicationprovisioning.Service,
) *controller.TartMachineReconciler {
	return &controller.TartMachineReconciler{
		Client:       k8sClient,
		Scheme:       scheme,
		HostService:  hostService,
		TokenService: tokenService,
		Provisioning: provisioningService,
	}
}

func provideTartClusterReconciler(k8sClient client.Client, scheme *runtime.Scheme) *controller.TartClusterReconciler {
	return &controller.TartClusterReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}
}

func provideTartMachineTemplateReconciler(k8sClient client.Client, scheme *runtime.Scheme) *controller.TartMachineTemplateReconciler {
	return &controller.TartMachineTemplateReconciler{
		Client: k8sClient,
		Scheme: scheme,
	}
}

func provideTartMachineV1Beta1Reconciler(
	k8sClient client.Client,
	hostReferences controller.HostReferenceService,
	nodeHealth controller.NodeHealthObserver,
	provisioner controller.ProvisionOrchestrator,
) *controller.TartMachineV1Beta1Reconciler {
	return &controller.TartMachineV1Beta1Reconciler{
		Client:         k8sClient,
		HostReferences: hostReferences,
		NodeHealth:     nodeHealth,
		Provisioner:    provisioner,
	}
}

func provideTartHostOperationReconciler(
	k8sClient client.Client,
	scheme *runtime.Scheme,
	powerOn controller.OperationPowerOnService,
	prepareBoot controller.OperationBootPreparationService,
	hostPhase controller.OperationHostPhaseService,
	targets controller.OperationDriverTargetBuilder,
	driverCapabilities controller.OperationDriverCapabilityObserver,
	driverPowerState controller.OperationDriverPowerStateObserver,
	driverBootState controller.OperationDriverBootStateObserver,
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
	tartHost *controller.TartHostReconciler,
	tartMachine *controller.TartMachineReconciler,
	tartCluster *controller.TartClusterReconciler,
	tartMachineTemplate *controller.TartMachineTemplateReconciler,
	tartMachineV1Beta1 *controller.TartMachineV1Beta1Reconciler,
	tartHostOperation *controller.TartHostOperationReconciler,
	driverService *applicationdriver.Service,
) Reconcilers {
	return Reconcilers{
		TartHost:            tartHost,
		TartMachine:         tartMachine,
		TartCluster:         tartCluster,
		TartMachineTemplate: tartMachineTemplate,
		TartMachineV1Beta1:  tartMachineV1Beta1,
		TartHostOperation:   tartHostOperation,
		Driver:              driverService,
	}
}

func InitializeReconcilers(k8sClient client.Client, scheme *runtime.Scheme) (Reconcilers, error) {
	wire.Build(
		k8shost.NewService,
		k8sbootstraptoken.NewService,
		k8sdrivercapability.NewService,
		k8sdriverstate.NewService,
		k8sdrivertarget.NewService,
		k8sallocation.NewService,
		k8smachinehealth.NewObserver,
		k8soperation.NewService,
		k8sv1beta1host.NewService,
		wire.Bind(new(applicationhost.Service), new(*k8shost.Service)),
		wire.Bind(new(applicationbootstraptoken.Service), new(*k8sbootstraptoken.Service)),
		wire.Bind(new(applicationprovisioning.HostReader), new(*k8shost.Service)),
		wire.Bind(new(applicationprovisioning.HostProvisioner), new(*k8shost.Service)),
		wire.Bind(new(applicationprovisioning.PowerOnService), new(*applicationdriver.Service)),

		// v1beta1 bindings
		appprovisioning.NewOrchestrator,
		wire.Bind(new(controller.HostReferenceService), new(*k8sallocation.Service)),
		wire.Bind(new(controller.NodeHealthObserver), new(*k8smachinehealth.Observer)),
		wire.Bind(new(controller.ProvisionOrchestrator), new(*appprovisioning.Orchestrator)),
		wire.Bind(new(appprovisioning.HostReserveService), new(*k8sallocation.Service)),
		wire.Bind(new(appprovisioning.HostPhaseService), new(*k8sv1beta1host.Service)),
		wire.Bind(new(appprovisioning.OperationService), new(*k8soperation.Service)),
		wire.Bind(new(controller.OperationPowerOnService), new(*applicationdriver.Service)),
		wire.Bind(new(controller.OperationBootPreparationService), new(*applicationdriver.Service)),
		wire.Bind(new(controller.OperationHostPhaseService), new(*k8sv1beta1host.Service)),
		wire.Bind(new(controller.OperationDriverTargetBuilder), new(*k8sdrivertarget.Service)),
		wire.Bind(new(controller.OperationDriverCapabilityObserver), new(*k8sdrivercapability.Service)),
		wire.Bind(new(controller.OperationDriverPowerStateObserver), new(*k8sdriverstate.Service)),
		wire.Bind(new(controller.OperationDriverBootStateObserver), new(*k8sdriverstate.Service)),
		wire.Bind(new(k8sdrivercapability.CapabilityDiscoverer), new(*applicationdriver.Service)),
		wire.Bind(new(k8sdrivercapability.HostCapabilityWriter), new(*k8sv1beta1host.Service)),
		wire.Bind(new(k8sdriverstate.PowerStateObserver), new(*applicationdriver.Service)),
		wire.Bind(new(k8sdriverstate.BootStateObserver), new(*applicationdriver.Service)),
		wire.Bind(new(k8sdriverstate.HostPowerStateWriter), new(*k8sv1beta1host.Service)),

		woladapter.Default,
		redfishadapter.New,
		provideDriverRegistry,
		applicationdriver.NewService,
		applicationprovisioning.NewService,
		provideTartHostReconciler,
		provideTartMachineReconciler,
		provideTartClusterReconciler,
		provideTartMachineTemplateReconciler,
		provideTartMachineV1Beta1Reconciler,
		provideTartHostOperationReconciler,
		provideReconcilers,
	)
	return Reconcilers{}, nil
}
