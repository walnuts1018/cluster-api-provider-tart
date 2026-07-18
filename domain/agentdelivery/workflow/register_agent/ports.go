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

package registeragent

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	agentsession "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/agentsession"
	agentprotocol "github.com/walnuts1018/cluster-api-provider-tart/dto/agent"
)

type OperationResolver interface {
	Resolve(context.Context, string) (client.ObjectKey, *infrastructurev1beta1.TartHostOperation, error)
}

type RegistrationVerifier interface {
	Verify(context.Context, *infrastructurev1beta1.TartHostOperation, string, agentprotocol.RegisterRequest) error
}

type SessionIssuer interface {
	Issue(context.Context, client.ObjectKey, string, string, time.Time) (agentsession.Token, time.Time, error)
}
