package host

import (
	"context"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
)

type Service interface {
	ReserveAvailable(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) (*infrastructurev1alpha1.TartHost, error)
	MarkProvisioning(ctx context.Context, host *infrastructurev1alpha1.TartHost) error
	ReleaseAssigned(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) error
	MarkAvailable(ctx context.Context, host *infrastructurev1alpha1.TartHost, reason, message string) error
	ReleaseMissingReference(ctx context.Context, host *infrastructurev1alpha1.TartHost) (bool, error)
}
