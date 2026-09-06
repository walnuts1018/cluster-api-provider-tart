package controller

import (
	"context"

	"github.com/walnuts1018/cluster-api-provider-tart/adapter/power"
	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

func (r *TartHostReconciler) powerOnHost(ctx context.Context, host *infrav1alpha1.TartHost) error {
	return power.PowerOnHost(ctx, r.Client, r.ManagementNamespace, host)
}

func (r *TartMachineReconciler) redfishPowerState(ctx context.Context, host *infrav1alpha1.TartHost) (power.PowerState, error) {
	return power.RedfishPowerState(ctx, r.Client, r.ManagementNamespace, host)
}
