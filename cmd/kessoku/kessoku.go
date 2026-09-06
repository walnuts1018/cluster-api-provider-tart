//go:generate go tool kessoku $GOFILE

package kessoku

import (
	kessokulib "github.com/mazrean/kessoku"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos/configbuilder"
	controller "github.com/walnuts1018/cluster-api-provider-tart/controller"
)

// Reconcilersはcmd/controller-managerへ接続するreconcilerを保持する。policy packageと外部adapterの依存関係をDI wiringへ限定し、controllerの実装をここへ置かない。
type Reconcilers struct {
	TartHost            *controller.TartHostReconciler
	TartCluster         *controller.TartClusterReconciler
	TartMachine         *controller.TartMachineReconciler
	TartBootstrapConfig *controller.TartBootstrapConfigReconciler
	TartControlPlane    *controller.TartControlPlaneReconciler
	TalosRecovery       *controller.TalosRecoveryReconciler
}

func provideTartHostReconciler(c client.Client) *controller.TartHostReconciler {
	return &controller.TartHostReconciler{Client: c}
}

func provideTartClusterReconciler(c client.Client) *controller.TartClusterReconciler {
	return &controller.TartClusterReconciler{Client: c}
}

func provideTartMachineReconciler(c client.Client) *controller.TartMachineReconciler {
	return &controller.TartMachineReconciler{Client: c}
}

func provideTartBootstrapConfigReconciler(c client.Client) *controller.TartBootstrapConfigReconciler {
	return &controller.TartBootstrapConfigReconciler{Client: c, Renderer: configbuilder.Builder{}}
}

func provideTartControlPlaneReconciler(c client.Client) *controller.TartControlPlaneReconciler {
	return &controller.TartControlPlaneReconciler{Client: c}
}

func provideTalosRecoveryReconciler(c client.Client) *controller.TalosRecoveryReconciler {
	return &controller.TalosRecoveryReconciler{Client: c}
}

func provideReconcilers(
	tartHost *controller.TartHostReconciler,
	tartCluster *controller.TartClusterReconciler,
	tartMachine *controller.TartMachineReconciler,
	tartBootstrapConfig *controller.TartBootstrapConfigReconciler,
	tartControlPlane *controller.TartControlPlaneReconciler,
	talosRecovery *controller.TalosRecoveryReconciler,
) Reconcilers {
	return Reconcilers{
		TartHost:            tartHost,
		TartCluster:         tartCluster,
		TartMachine:         tartMachine,
		TartBootstrapConfig: tartBootstrapConfig,
		TartControlPlane:    tartControlPlane,
		TalosRecovery:       talosRecovery,
	}
}

var _ = kessokulib.Inject[Reconcilers](
	"InitializeReconcilers",
	kessokulib.Provide(provideTartHostReconciler),
	kessokulib.Provide(provideTartClusterReconciler),
	kessokulib.Provide(provideTartMachineReconciler),
	kessokulib.Provide(provideTartBootstrapConfigReconciler),
	kessokulib.Provide(provideTartControlPlaneReconciler),
	kessokulib.Provide(provideTalosRecoveryReconciler),
	kessokulib.Provide(provideReconcilers),
)
