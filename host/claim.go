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

// ErrInvalidClaim indicates that a claim cannot identify a namespaced consumer safely.
var ErrInvalidClaim = errors.New("invalid host claim")

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

func validConsumerReference(consumer corev1.ObjectReference) bool {
	return consumer.APIVersion != "" && consumer.Kind != "" && consumer.Namespace != "" && consumer.Name != "" && consumer.UID != ""
}
