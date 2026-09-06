package host

import (
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

// ErrClaimConflictは取得後にconsumerRefが変化した場合の競合を表す。
var ErrClaimConflict = errors.New("host claim conflict")

// ErrInvalidClaimはnamespaced consumerを安全に識別できないためclaimできないことを示す。
var ErrInvalidClaim = errors.New("invalid host claim")

// ErrInvalidRetentionは停止確認済みのconsumerをretention recordへ変換できないことを示す。
var ErrInvalidRetention = errors.New("invalid host retention")

// ClaimDecisionはclaim/retain要求を現在のspecへ適用すべきかを表す判定結果である。
type ClaimDecision int

const (
	// ClaimNoopは既に要求どおりの状態へ収束しているため、spec更新が不要であることを示す。
	ClaimNoop ClaimDecision = iota
	// ClaimApplyはspec更新が必要であることを示す。
	ClaimApply
)

// DecideClaimはHostの現在のconsumerRefとclaim要求を比較し、適用可否をCASの前提となる判定として返す。
// Kubernetes clientへの副作用は一切行わない。
func DecideClaim(spec infrav1alpha1.TartHostSpec, consumer corev1.ObjectReference) (ClaimDecision, error) {
	if !ValidConsumerReference(consumer) {
		return ClaimNoop, ErrInvalidClaim
	}
	if spec.ConsumerRef != nil && *spec.ConsumerRef != consumer {
		return ClaimNoop, fmt.Errorf("%w: host is already claimed by %s/%s", ErrClaimConflict, spec.ConsumerRef.Namespace, spec.ConsumerRef.Name)
	}
	if spec.ConsumerRef != nil {
		return ClaimNoop, nil
	}
	return ClaimApply, nil
}

// DecideRetentionは現在のconsumer bindingをpreviousConsumerRefへ移せるかを判定する。
// claimが別consumerへ変化していた場合や、既にretention済みの場合はClaimNoopまたはエラーを返す。
func DecideRetention(spec infrav1alpha1.TartHostSpec, consumer corev1.ObjectReference, previous infrav1alpha1.PreviousConsumerRef) (ClaimDecision, error) {
	if !ValidConsumerReference(consumer) || previous.UID == "" || previous.UID != consumer.UID {
		return ClaimNoop, ErrInvalidRetention
	}
	if spec.ConsumerRef == nil {
		if spec.PreviousConsumerRef != nil && spec.PreviousConsumerRef.UID == previous.UID {
			return ClaimNoop, nil
		}
		return ClaimNoop, fmt.Errorf("%w: host is no longer claimed by the deleting Machine", ErrClaimConflict)
	}
	if *spec.ConsumerRef != consumer {
		return ClaimNoop, fmt.Errorf("%w: host is claimed by another consumer", ErrClaimConflict)
	}
	return ClaimApply, nil
}

// ValidConsumerReferenceはconsumerがnamespaced object referenceとして安全に識別可能かを返す。
func ValidConsumerReference(consumer corev1.ObjectReference) bool {
	return consumer.APIVersion != "" && consumer.Kind != "" && consumer.Namespace != "" && consumer.Name != "" && consumer.UID != ""
}
