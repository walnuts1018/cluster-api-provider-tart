//go:generate go tool kessoku $GOFILE

package kessoku

import (
	kessokulib "github.com/mazrean/kessoku"

	"github.com/walnuts1018/cluster-api-provider-tart/controller/talosrecovery"
	"github.com/walnuts1018/cluster-api-provider-tart/controller/tartcluster"
	"github.com/walnuts1018/cluster-api-provider-tart/controller/tarthost"
	"github.com/walnuts1018/cluster-api-provider-tart/controller/tartmachine"
)

// InfrastructureReconcilersはcmd/infrastructure-managerへ接続するreconcilerを保持する。policy packageと外部adapterの依存関係をDI wiringへ限定し、controllerの実装をここへ置かない。
type InfrastructureReconcilers struct {
	TartHost      *tarthost.TartHostReconciler
	TartCluster   *tartcluster.TartClusterReconciler
	TartMachine   *tartmachine.TartMachineReconciler
	TalosRecovery *talosrecovery.TalosRecoveryReconciler
}

func provideInfrastructureReconcilers(
	tartHost *tarthost.TartHostReconciler,
	tartCluster *tartcluster.TartClusterReconciler,
	tartMachine *tartmachine.TartMachineReconciler,
	talosRecovery *talosrecovery.TalosRecoveryReconciler,
) InfrastructureReconcilers {
	return InfrastructureReconcilers{
		TartHost:      tartHost,
		TartCluster:   tartCluster,
		TartMachine:   tartMachine,
		TalosRecovery: talosRecovery,
	}
}

// infrastructureReconcilerProvidersは、clientのみを受け取ってreconcilerを構築する単純な
// コンストラクタ群をまとめたsetである。このファイル内でのみ再利用する。
var infrastructureReconcilerProviders = kessokulib.Set(
	kessokulib.Provide(tarthost.NewTartHostReconciler),
	kessokulib.Provide(tartcluster.NewTartClusterReconciler),
	kessokulib.Provide(tartmachine.NewTartMachineReconciler),
	kessokulib.Provide(talosrecovery.NewTalosRecoveryReconciler),
)

var _ = kessokulib.Inject[InfrastructureReconcilers](
	"InitializeInfrastructureReconcilers",
	infrastructureReconcilerProviders,
	kessokulib.Provide(provideInfrastructureReconcilers),
)
