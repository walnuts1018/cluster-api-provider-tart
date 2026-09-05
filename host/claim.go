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

// ErrClaimConflictは取得後にconsumerRefが変化した場合の競合を表す。
var ErrClaimConflict = errors.New("host claim conflict")

// ClaimはresourceVersion付きUpdateでhost.spec.consumerRefをatomicに確立する。
// 既存claimが別consumerを指す場合は上書きせず、呼び出し側が再選択できる競合として返す。
func Claim(ctx context.Context, c client.Client, host *infrav1alpha1.TartHost, consumer corev1.ObjectReference) error {
	if host.Spec.ConsumerRef != nil && host.Spec.ConsumerRef.UID != consumer.UID {
		return fmt.Errorf("%w: host %s is already claimed by %s/%s", ErrClaimConflict, host.Name, host.Spec.ConsumerRef.Namespace, host.Spec.ConsumerRef.Name)
	}
	if host.Spec.ConsumerRef != nil {
		return nil
	}

	claimed := host.DeepCopy()
	claimed.Spec.ConsumerRef = &consumer
	if err := c.Update(ctx, claimed); err != nil {
		if apierrors.IsConflict(err) {
			return fmt.Errorf("%w: %w", ErrClaimConflict, err)
		}
		return err
	}
	*host = *claimed
	return nil
}
