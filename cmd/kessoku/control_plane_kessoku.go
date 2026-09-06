//go:generate go tool kessoku $GOFILE

package kessoku

import (
	kessokulib "github.com/mazrean/kessoku"

	"github.com/walnuts1018/cluster-api-provider-tart/controller/tartcontrolplane"
)

// ControlPlaneReconcilersはcmd/control-plane-managerへ接続するreconcilerを保持する。policy packageと外部adapterの依存関係をDI wiringへ限定し、controllerの実装をここへ置かない。
type ControlPlaneReconcilers struct {
	TartControlPlane *tartcontrolplane.TartControlPlaneReconciler
}

func provideControlPlaneReconcilers(
	tartControlPlane *tartcontrolplane.TartControlPlaneReconciler,
) ControlPlaneReconcilers {
	return ControlPlaneReconcilers{
		TartControlPlane: tartControlPlane,
	}
}

var _ = kessokulib.Inject[ControlPlaneReconcilers](
	"InitializeControlPlaneReconcilers",
	kessokulib.Provide(tartcontrolplane.NewTartControlPlaneReconciler),
	kessokulib.Provide(provideControlPlaneReconcilers),
)
