//go:generate go tool kessoku $GOFILE

package kessoku

import (
	kessokulib "github.com/mazrean/kessoku"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/talos/configbuilder"
	"github.com/walnuts1018/cluster-api-provider-tart/controller/tartbootstrapconfig"
	"github.com/walnuts1018/cluster-api-provider-tart/usecase/bootstrap"
)

// BootstrapReconcilersはcmd/bootstrap-managerへ接続するreconcilerを保持する。policy packageと外部adapterの依存関係をDI wiringへ限定し、controllerの実装をここへ置かない。
type BootstrapReconcilers struct {
	TartBootstrapConfig *tartbootstrapconfig.TartBootstrapConfigReconciler
}

func provideBootstrapReconcilers(
	tartBootstrapConfig *tartbootstrapconfig.TartBootstrapConfigReconciler,
) BootstrapReconcilers {
	return BootstrapReconcilers{
		TartBootstrapConfig: tartBootstrapConfig,
	}
}

var _ = kessokulib.Inject[BootstrapReconcilers](
	"InitializeBootstrapReconcilers",
	// configbuilder.Builderは具象型であり、tartbootstrapconfig.NewTartBootstrapConfigReconcilerは
	// bootstrap.ConfigRenderer interfaceを要求するため、Bindでinterfaceへ結び付ける。
	kessokulib.Bind[bootstrap.ConfigRenderer](kessokulib.Provide(configbuilder.NewBuilder)),
	kessokulib.Provide(tartbootstrapconfig.NewTartBootstrapConfigReconciler),
	kessokulib.Provide(provideBootstrapReconcilers),
)
