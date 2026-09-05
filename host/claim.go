// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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

// ErrClaimConflict is returned when a Host's consumerRef changed between the caller's
// read and the claim attempt, either because another Machine claimed it first or
// because the caller's cached copy was stale. Callers must not retry against the same
// Host; they must re-select from current observed state instead.
var ErrClaimConflict = errors.New("host claim conflict")

// Claim atomically binds host.spec.consumerRef to consumer using a
// resourceVersion-checked Update, never server-side apply. It only succeeds if the
// Host's consumerRef is currently nil or already equal to consumer; a claim already
// held by a different consumer is reported as ErrClaimConflict rather than overwritten.
//
// TODO: 次セッションで、resourceVersion競合時の別Host選択・再試行フローをhost選択pure
// 関数と組み合わせて実装する。ここでは単一Hostに対する原子的なCASだけを提供する。
func Claim(ctx context.Context, c client.Client, host *infrav1alpha1.TartHost, consumer corev1.ObjectReference) error {
	if host.Spec.ConsumerRef != nil && host.Spec.ConsumerRef.UID != consumer.UID {
		return fmt.Errorf("%w: host %s is already claimed by %s/%s", ErrClaimConflict, host.Name, host.Spec.ConsumerRef.Namespace, host.Spec.ConsumerRef.Name)
	}
	original := host.DeepCopy()
	host.Spec.ConsumerRef = &consumer
	if err := c.Patch(ctx, host, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
		if apierrors.IsConflict(err) {
			return fmt.Errorf("%w: %w", ErrClaimConflict, err)
		}
		return err
	}
	return nil
}
