//go:build wireinject

package wire

import (
	"github.com/google/wire"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	woladapter "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/driver/wol"
	k8sbootstraptoken "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/bootstraptoken"
	k8shost "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/host"
	applicationbootstraptoken "github.com/walnuts1018/cluster-api-provider-tart/internal/application/bootstraptoken"
	applicationdriver "github.com/walnuts1018/cluster-api-provider-tart/internal/application/driver"
	applicationhost "github.com/walnuts1018/cluster-api-provider-tart/internal/application/host"
	applicationprovisioning "github.com/walnuts1018/cluster-api-provider-tart/internal/application/provisioning"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/controller"
	driverdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/driver"
)

type Reconcilers struct {
	TartHost            *controller.TartHostReconciler
	TartMachine         *controller.TartMachineReconciler
	TartCluster         *controller.TartClusterReconciler
	TartMachineTemplate *controller.TartMachineTemplateReconciler
}

func provideDriverRegistry(wolDriver *woladapter.Adapter) (*applicationdriver.Registry, error) {
	registry := applicationdriver.NewRegistry()
	if err := registry.RegisterPowerOn(driverdomain.WoL, wolDriver); err != nil {
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

func provideReconcilers(
	tartHost *controller.TartHostReconciler,
	tartMachine *controller.TartMachineReconciler,
	tartCluster *controller.TartClusterReconciler,
	tartMachineTemplate *controller.TartMachineTemplateReconciler,
) Reconcilers {
	return Reconcilers{
		TartHost:            tartHost,
		TartMachine:         tartMachine,
		TartCluster:         tartCluster,
		TartMachineTemplate: tartMachineTemplate,
	}
}

func InitializeReconcilers(k8sClient client.Client, scheme *runtime.Scheme) (Reconcilers, error) {
	wire.Build(
		k8shost.NewService,
		k8sbootstraptoken.NewService,
		wire.Bind(new(applicationhost.Service), new(*k8shost.Service)),
		wire.Bind(new(applicationbootstraptoken.Service), new(*k8sbootstraptoken.Service)),
		wire.Bind(new(applicationprovisioning.HostReader), new(*k8shost.Service)),
		wire.Bind(new(applicationprovisioning.HostProvisioner), new(*k8shost.Service)),
		wire.Bind(new(applicationprovisioning.PowerOnService), new(*applicationdriver.Service)),
		woladapter.Default,
		provideDriverRegistry,
		applicationdriver.NewService,
		applicationprovisioning.NewService,
		provideTartHostReconciler,
		provideTartMachineReconciler,
		provideTartClusterReconciler,
		provideTartMachineTemplateReconciler,
		provideReconcilers,
	)
	return Reconcilers{}, nil
}
