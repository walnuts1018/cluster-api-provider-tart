package host

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

// ErrClaimConflictは取得後にconsumerRefが変化した場合の競合を表す。
var ErrClaimConflict = errors.New("host claim conflict")

// ErrInvalidClaimはnamespaced consumerを安全に識別できないためclaimできないことを示す。
var ErrInvalidClaim = errors.New("invalid host claim")

// ErrInvalidRetentionは停止確認済みのconsumerをretention recordへ変換できないことを示す。
var ErrInvalidRetention = errors.New("invalid host retention")

const claimAttempts = 3

// ClaimはresourceVersion付きUpdateでhost.spec.consumerRefをatomicに確立する。
// 既存claimが別consumerを指す場合は上書きせず、呼び出し側が再選択できる競合として返す。
func Claim(ctx context.Context, c client.Client, host *infrav1alpha1.TartHost, consumer corev1.ObjectReference) error {
	if c == nil || host == nil || host.Name == "" || !validConsumerReference(consumer) {
		return ErrInvalidClaim
	}
	for attempt := range claimAttempts {
		if host.Spec.ConsumerRef != nil && *host.Spec.ConsumerRef != consumer {
			return fmt.Errorf("%w: host %s is already claimed by %s/%s", ErrClaimConflict, host.Name, host.Spec.ConsumerRef.Namespace, host.Spec.ConsumerRef.Name)
		}
		if host.Spec.ConsumerRef != nil {
			return nil
		}

		claimed := host.DeepCopy()
		claimed.Spec.ConsumerRef = &consumer
		err := c.Update(ctx, claimed)
		if err == nil {
			*host = *claimed
			return nil
		}
		if !apierrors.IsConflict(err) {
			return err
		}
		if attempt == claimAttempts-1 {
			return fmt.Errorf("%w: host update conflicted after %d attempts: %w", ErrClaimConflict, claimAttempts, err)
		}

		refreshed := &infrav1alpha1.TartHost{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(host), refreshed); err != nil {
			return err
		}
		*host = *refreshed
	}

	return fmt.Errorf("%w: host claim attempts exhausted", ErrClaimConflict)
}

// Retainは現在のconsumer bindingを前回consumer recordへ移し、Hostを明示的なreuse approval待ちにする。claimが別consumerへ変化していた場合はretentionを実行しない。
func Retain(ctx context.Context, c client.Client, host *infrav1alpha1.TartHost, consumer corev1.ObjectReference, previous infrav1alpha1.PreviousConsumerRef) error {
	if c == nil || host == nil || host.Name == "" || !validConsumerReference(consumer) || previous.UID == "" || previous.UID != consumer.UID {
		return ErrInvalidRetention
	}
	for attempt := range claimAttempts {
		if host.Spec.ConsumerRef == nil {
			if host.Spec.PreviousConsumerRef != nil && host.Spec.PreviousConsumerRef.UID == previous.UID {
				return nil
			}
			return fmt.Errorf("%w: host %s is no longer claimed by the deleting Machine", ErrClaimConflict, host.Name)
		}
		if *host.Spec.ConsumerRef != consumer {
			return fmt.Errorf("%w: host %s is claimed by another consumer", ErrClaimConflict, host.Name)
		}

		retained := host.DeepCopy()
		retained.Spec.ConsumerRef = nil
		retained.Spec.PreviousConsumerRef = new(previous)
		if err := c.Update(ctx, retained); err == nil {
			*host = *retained
			return nil
		} else if !apierrors.IsConflict(err) {
			return err
		} else if attempt == claimAttempts-1 {
			return fmt.Errorf("%w: host retention conflicted after %d attempts: %w", ErrClaimConflict, claimAttempts, err)
		}

		refreshed := &infrav1alpha1.TartHost{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(host), refreshed); err != nil {
			return err
		}
		*host = *refreshed
	}

	return fmt.Errorf("%w: host retention attempts exhausted", ErrClaimConflict)
}

func validConsumerReference(consumer corev1.ObjectReference) bool {
	return consumer.APIVersion != "" && consumer.Kind != "" && consumer.Namespace != "" && consumer.Name != "" && consumer.UID != ""
}
