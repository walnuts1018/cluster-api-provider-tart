// Package kubernetesはcontroller-runtime clientを使ってusecase portを満たすrepository実装を提供する。
package kubernetes

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
	hostusecase "github.com/walnuts1018/cluster-api-provider-tart/usecase/host"
)

// claimAttemptsはresourceVersion conflict発生時にHostを再取得して判定をやり直す最大回数である。
const claimAttempts = 3

// TartHostRepositoryはusecase/host.HostRepositoryの実装で、resourceVersion付きUpdateによる
// optimistic concurrency(CAS)でTartHost.spec.consumerRefをatomicに更新する。
type TartHostRepository struct {
	Client client.Client
}

// NewTartHostRepositoryはclientを保持するTartHostRepositoryを構築する。
func NewTartHostRepository(c client.Client) TartHostRepository {
	return TartHostRepository{Client: c}
}

// ClaimHostは既存claimが別consumerを指す場合は上書きせず、呼び出し側が再選択できる競合として返す。
func (r TartHostRepository) ClaimHost(ctx context.Context, host *infrav1alpha1.TartHost, consumer corev1.ObjectReference) error {
	if r.Client == nil || host == nil || host.Name == "" {
		return hostusecase.ErrInvalidClaim
	}
	for attempt := range claimAttempts {
		decision, err := hostusecase.DecideClaim(host.Spec, consumer)
		if err != nil {
			return err
		}
		if decision == hostusecase.ClaimNoop {
			return nil
		}

		claimed := host.DeepCopy()
		claimed.Spec.ConsumerRef = &consumer
		if err := r.Client.Update(ctx, claimed); err == nil {
			*host = *claimed
			return nil
		} else if !apierrors.IsConflict(err) {
			return err
		} else if attempt == claimAttempts-1 {
			return fmt.Errorf("%w: host update conflicted after %d attempts: %w", hostusecase.ErrClaimConflict, claimAttempts, err)
		}

		refreshed := &infrav1alpha1.TartHost{}
		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(host), refreshed); err != nil {
			return err
		}
		*host = *refreshed
	}

	return fmt.Errorf("%w: host claim attempts exhausted", hostusecase.ErrClaimConflict)
}

// RetainHostは現在のconsumer bindingを前回consumer recordへ移し、Hostを明示的なreuse approval待ちにする。
func (r TartHostRepository) RetainHost(ctx context.Context, host *infrav1alpha1.TartHost, consumer corev1.ObjectReference, previous infrav1alpha1.PreviousConsumerRef) error {
	if r.Client == nil || host == nil || host.Name == "" {
		return hostusecase.ErrInvalidRetention
	}
	for attempt := range claimAttempts {
		decision, err := hostusecase.DecideRetention(host.Spec, consumer, previous)
		if err != nil {
			return err
		}
		if decision == hostusecase.ClaimNoop {
			return nil
		}

		retained := host.DeepCopy()
		retained.Spec.ConsumerRef = nil
		retained.Spec.PreviousConsumerRef = &previous
		if err := r.Client.Update(ctx, retained); err == nil {
			*host = *retained
			return nil
		} else if !apierrors.IsConflict(err) {
			return err
		} else if attempt == claimAttempts-1 {
			return fmt.Errorf("%w: host retention conflicted after %d attempts: %w", hostusecase.ErrClaimConflict, claimAttempts, err)
		}

		refreshed := &infrav1alpha1.TartHost{}
		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(host), refreshed); err != nil {
			return err
		}
		*host = *refreshed
	}

	return fmt.Errorf("%w: host retention attempts exhausted", hostusecase.ErrClaimConflict)
}
