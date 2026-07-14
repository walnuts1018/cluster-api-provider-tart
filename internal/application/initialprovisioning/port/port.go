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

package port

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	agentsessiondomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/agentsession"
	allocationdomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/allocation"
)

type HostReserveService interface {
	Reserve(
		ctx context.Context,
		machine *infrastructurev1beta1.TartMachine,
		requirements allocationdomain.Requirements,
	) (*infrastructurev1beta1.TartHost, error)
}

type HostPhaseService interface {
	ReserveForMachine(ctx context.Context, host *infrastructurev1beta1.TartHost, machine *infrastructurev1beta1.TartMachine) error
	MarkHostProvisioned(ctx context.Context, host *infrastructurev1beta1.TartHost) error
}

type OperationService interface {
	Start(ctx context.Context, desired *infrastructurev1beta1.TartHostOperation) (*infrastructurev1beta1.TartHostOperation, error)
	CompleteProvision(ctx context.Context, operation *infrastructurev1beta1.TartHostOperation) error
}

type SessionTokenIssuer interface {
	Issue(ctx context.Context, key client.ObjectKey, hostUID, operationUID string, now time.Time) (agentsessiondomain.Token, time.Time, error)
}
