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

	infrastructurev1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1alpha1"
)

type Service interface {
	ReserveAvailable(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) (*infrastructurev1alpha1.TartHost, error)
	MarkProvisioning(ctx context.Context, host *infrastructurev1alpha1.TartHost) error
	MarkProvisioned(ctx context.Context, host *infrastructurev1alpha1.TartHost) error
	GetAssigned(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) (*infrastructurev1alpha1.TartHost, error)
	ReleaseAssigned(ctx context.Context, machine *infrastructurev1alpha1.TartMachine) error
	MarkAvailable(ctx context.Context, host *infrastructurev1alpha1.TartHost, reason, message string) error
	ReleaseMissingReference(ctx context.Context, host *infrastructurev1alpha1.TartHost) (bool, error)
}
