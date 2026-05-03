//go:build wireinject
// +build wireinject

package main

import (
	"github.com/google/wire"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/walnuts1018/cluster-api-provider-tart/internal/controller"
	hostdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/host"
	provisioningdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/provisioning"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/wol"
)

type Reconcilers struct {
	TartHost    *controller.TartHostReconciler
	TartMachine *controller.TartMachineReconciler
}

func provideWakeOnLANSender() provisioningdomain.WakeOnLANSender {
	return wol.DefaultSender()
}

func provideTartHostReconciler(k8sClient client.Client, scheme *runtime.Scheme, hostService hostdomain.Service) *controller.TartHostReconciler {
	return &controller.TartHostReconciler{
		Client:      k8sClient,
		Scheme:      scheme,
		HostService: hostService,
	}
}

func provideTartMachineReconciler(
	k8sClient client.Client,
	scheme *runtime.Scheme,
	hostService hostdomain.Service,
	provisioningService provisioningdomain.Service,
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
		hostdomain.NewService,
		wire.Bind(new(provisioningdomain.HostService), new(hostdomain.Service)),
		provideWakeOnLANSender,
		provisioningdomain.NewService,
		provideTartHostReconciler,
		provideTartMachineReconciler,
		provideReconcilers,
	)
	return Reconcilers{}, nil
}
