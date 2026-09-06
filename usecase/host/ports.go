// Package hostはTartHostのclaim/retainを扱うusecaseと、そのport定義を提供する。
package host

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	infrav1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/infrastructure/v1alpha1"
)

// HostRepositoryはTartHost.spec.consumerRefへのatomicなclaim/retain更新を隔離するport。
// Kubernetes API serverとの通信(optimistic concurrencyによるCAS retry)はこのportの実装側に閉じ込め、
// usecaseはdomain/hostの判定結果を意識しない。
type HostRepository interface {
	// ClaimHostはHostのconsumerRefをCASで確立する。既に同じconsumerでclaim済みの場合は何もしない。
	ClaimHost(ctx context.Context, host *infrav1alpha1.TartHost, consumer corev1.ObjectReference) error
	// RetainHostは現在のconsumer bindingをpreviousConsumerRefへ移し、Hostを明示的なreuse approval待ちにする。
	RetainHost(ctx context.Context, host *infrav1alpha1.TartHost, consumer corev1.ObjectReference, previous infrav1alpha1.PreviousConsumerRef) error
}
