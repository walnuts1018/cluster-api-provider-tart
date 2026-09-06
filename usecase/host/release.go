package host

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

// RetainHostUseCaseはHostRepositoryを介して現在のconsumer bindingをpreviousConsumerRefへ移す。
type RetainHostUseCase struct {
	Repository HostRepository
}

// ExecuteはMachine削除時にHostのconsumerRefを解除し、previousConsumerRefへ記録する。
func (u RetainHostUseCase) Execute(ctx context.Context, host *infrav1alpha1.TartHost, consumer corev1.ObjectReference, previous infrav1alpha1.PreviousConsumerRef) error {
	return u.Repository.RetainHost(ctx, host, consumer, previous)
}
