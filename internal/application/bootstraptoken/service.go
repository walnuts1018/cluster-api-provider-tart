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

package bootstraptoken

import (
	"context"

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
	onetimetoken "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/onetime_token"
)

type Service interface {
	Ensure(ctx context.Context, machine *infrastructurev1alpha1.TartMachine, token onetimetoken.OneTimeToken) error
	Get(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) (onetimetoken.OneTimeToken, bool, error)
	Delete(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) error
}
