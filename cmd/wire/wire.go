//go:build wireinject
// +build wireinject

package wire

import (
	"github.com/google/wire"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	k8shost "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/k8s/host"
	applicationhost "github.com/walnuts1018/cluster-api-provider-tart/internal/application/host"
	applicationprovisioning "github.com/walnuts1018/cluster-api-provider-tart/internal/application/provisioning"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/controller"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/wol"
)

type Reconcilers struct {
	TartHost    *controller.TartHostReconciler
	TartMachine *controller.TartMachineReconciler
}

func provideWakeOnLANSender() applicationprovisioning.WakeOnLANSender {
	return wol.DefaultSender()
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
	provisioningService applicationprovisioning.Service,
) *controller.TartMachineReconciler {
	return &controller.TartMachineReconciler{
		Client:       k8sClient,
		Scheme:       scheme,
		HostService:  hostService,
		Provisioning: provisioningService,
	}
}

func provideReconcilers(
	tartHost *controller.TartHostReconciler,
	tartMachine *controller.TartMachineReconciler,
) Reconcilers {
	return Reconcilers{
		TartHost:    tartHost,
		TartMachine: tartMachine,
	}
}

func InitializeReconcilers(k8sClient client.Client, scheme *runtime.Scheme) (Reconcilers, error) {
	wire.Build(
		k8shost.NewService,
		wire.Bind(new(applicationhost.Service), new(*k8shost.Service)),
		wire.Bind(new(applicationprovisioning.HostReader), new(*k8shost.Service)),
		wire.Bind(new(applicationprovisioning.HostProvisioner), new(*k8shost.Service)),
		provideWakeOnLANSender,
		applicationprovisioning.NewService,
		provideTartHostReconciler,
		provideTartMachineReconciler,
		provideReconcilers,
	)
	return Reconcilers{}, nil
}
