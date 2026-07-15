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

package securedelivery

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	agentsessiondomain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/agentsession"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

type OperationResolver interface {
	Resolve(context.Context, string) (client.ObjectKey, *infrastructurev1beta1.TartHostOperation, error)
}

type RegistrationVerifier interface {
	Verify(context.Context, *infrastructurev1beta1.TartHostOperation, string, agentprotocol.RegisterRequest) error
}

type SessionService interface {
	Issue(context.Context, client.ObjectKey, string, string, time.Time) (agentsessiondomain.Token, time.Time, error)
	Authenticate(context.Context, client.ObjectKey, string, string, string, time.Time) error
	ClaimBootstrap(context.Context, client.ObjectKey, string, string, string, time.Time) error
}

type BootstrapProvider interface {
	GetBootstrapBundle(context.Context, client.ObjectKey) (agentprotocol.BootstrapBundle, error)
}

type Ports struct {
	Operations           OperationResolver
	RegistrationVerifier RegistrationVerifier
	Sessions             SessionService
	Bootstrap            BootstrapProvider
	Now                  func() time.Time
}
