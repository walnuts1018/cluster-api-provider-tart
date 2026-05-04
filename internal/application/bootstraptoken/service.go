package bootstraptoken

import (
	"context"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	onetimetoken "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/onetime_token"
)

type Service interface {
	Ensure(ctx context.Context, machine *infrastructurev1alpha1.TartMachine, token onetimetoken.OneTimeToken) error
	Get(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) (onetimetoken.OneTimeToken, bool, error)
	Delete(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) error
}
