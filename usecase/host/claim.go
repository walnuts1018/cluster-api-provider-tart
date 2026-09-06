package host

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

// ClaimHostUseCaseはHostRepositoryを介してHostのconsumerRefをclaimする。
type ClaimHostUseCase struct {
	Repository HostRepository
}

// ExecuteはHostをconsumerへclaimする。判定ロジック自体はdomain/host、副作用はHostRepositoryへ委譲する。
func (u ClaimHostUseCase) Execute(ctx context.Context, host *infrav1alpha1.TartHost, consumer corev1.ObjectReference) error {
	return u.Repository.ClaimHost(ctx, host, consumer)
}
