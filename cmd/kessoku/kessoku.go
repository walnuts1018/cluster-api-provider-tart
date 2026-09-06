//go:generate go tool kessoku $GOFILE

package kessoku

import (
	kessokulib "github.com/mazrean/kessoku"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos/configbuilder"
	"github.com/walnuts1018/cluster-api-provider-tart/controller/talosrecovery"
	"github.com/walnuts1018/cluster-api-provider-tart/controller/tartbootstrapconfig"
	"github.com/walnuts1018/cluster-api-provider-tart/controller/tartcluster"
	"github.com/walnuts1018/cluster-api-provider-tart/controller/tartcontrolplane"
	"github.com/walnuts1018/cluster-api-provider-tart/controller/tarthost"
	"github.com/walnuts1018/cluster-api-provider-tart/controller/tartmachine"
)

// Reconcilersはcmd/controller-managerへ接続するreconcilerを保持する。policy packageと外部adapterの依存関係をDI wiringへ限定し、controllerの実装をここへ置かない。
type Reconcilers struct {
	TartHost            *tarthost.TartHostReconciler
	TartCluster         *tartcluster.TartClusterReconciler
	TartMachine         *tartmachine.TartMachineReconciler
	TartBootstrapConfig *tartbootstrapconfig.TartBootstrapConfigReconciler
	TartControlPlane    *tartcontrolplane.TartControlPlaneReconciler
	TalosRecovery       *talosrecovery.TalosRecoveryReconciler
}

func provideTartHostReconciler(c client.Client) *tarthost.TartHostReconciler {
	return &tarthost.TartHostReconciler{Client: c}
}

func provideTartClusterReconciler(c client.Client) *tartcluster.TartClusterReconciler {
	return &tartcluster.TartClusterReconciler{Client: c}
}

func provideTartMachineReconciler(c client.Client) *tartmachine.TartMachineReconciler {
	return &tartmachine.TartMachineReconciler{Client: c}
}

func provideTartBootstrapConfigReconciler(c client.Client) *tartbootstrapconfig.TartBootstrapConfigReconciler {
	return &tartbootstrapconfig.TartBootstrapConfigReconciler{Client: c, Renderer: configbuilder.Builder{}}
}

func provideTartControlPlaneReconciler(c client.Client) *tartcontrolplane.TartControlPlaneReconciler {
	return &tartcontrolplane.TartControlPlaneReconciler{Client: c}
}

func provideTalosRecoveryReconciler(c client.Client) *talosrecovery.TalosRecoveryReconciler {
	return &talosrecovery.TalosRecoveryReconciler{Client: c}
}

func provideReconcilers(
	tartHost *tarthost.TartHostReconciler,
	tartCluster *tartcluster.TartClusterReconciler,
	tartMachine *tartmachine.TartMachineReconciler,
	tartBootstrapConfig *tartbootstrapconfig.TartBootstrapConfigReconciler,
	tartControlPlane *tartcontrolplane.TartControlPlaneReconciler,
	talosRecovery *talosrecovery.TalosRecoveryReconciler,
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
