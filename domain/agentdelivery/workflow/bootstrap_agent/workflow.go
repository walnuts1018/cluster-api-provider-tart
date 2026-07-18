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

package bootstrapagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	securedelivery "github.com/walnuts1018/cluster-api-provider-tart/domain/agentdelivery/entity/securedelivery"
	sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"
	sharedworkflow "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/workflow"
	agentprotocol "github.com/walnuts1018/cluster-api-provider-tart/dto/agent"
)

type Result = securedelivery.Result
type Accepted = securedelivery.BootstrapAccepted
type Rejected = securedelivery.BootstrapRejected

type Command struct {
	OperationUID  string
	Authorization string
}

type Event interface {
	isEvent()
}

type BootstrapDelivered struct {
	Bundle agentprotocol.BootstrapBundle
}

type BootstrapRejected struct {
	Status  int
	Code    string
	Message string
}

func (BootstrapDelivered) isEvent() {}
func (BootstrapRejected) isEvent()  {}

type Workflow struct {
	operations OperationResolver
	sessions   SessionClaimer
	bootstrap  BootstrapProvider
	now        func() time.Time
}

func NewWorkflow(
	operations OperationResolver,
	sessions SessionClaimer,
	bootstrap BootstrapProvider,
	now func() time.Time,
) *Workflow {
	if now == nil {
		now = time.Now
	}
	return &Workflow{operations: operations, sessions: sessions, bootstrap: bootstrap, now: now}
}

func (workflow *Workflow) Do(ctx context.Context, command Command) sharedresult.Result[Event, sharedworkflow.Failure] {
	result, err := workflow.execute(ctx, command)
	if err != nil {
		return sharedworkflow.Failed[Event](sharedworkflow.DependencyFailure{Operation: "deliver Bootstrap", Detail: err.Error()})
	}
	switch result := result.(type) {
	case Accepted:
		return sharedworkflow.Succeeded[Event](BootstrapDelivered{Bundle: result.Bundle})
	case Rejected:
		return sharedworkflow.Succeeded[Event](BootstrapRejected{Status: result.Status, Code: result.Code, Message: result.Message})
	default:
		panic(fmt.Sprintf("unknown Bootstrap result: %T", result))
	}
}

func (workflow *Workflow) execute(ctx context.Context, command Command) (Result, error) {
	if workflow == nil || workflow.operations == nil || workflow.sessions == nil || workflow.bootstrap == nil {
		return nil, fmt.Errorf("bootstrap Agent workflow dependencies are required")
	}
	key, operation, err := workflow.operations.Resolve(ctx, command.OperationUID)
	if err != nil || operation == nil || operation.Spec.OperationID != command.OperationUID {
		return Rejected{Status: 401, Code: "unauthorized", Message: "Authentication failed"}, nil //nolint:nilerr // Operationの存在を認証前に外部へ漏らさない。
	}
	token, ok := bearerToken(command.Authorization)
	if !ok {
		return Rejected{Status: 401, Code: "unauthorized", Message: "Authentication failed"}, nil
	}
	bundle, err := workflow.bootstrap.GetBootstrapBundle(ctx, key)
	if errors.Is(err, agentprotocol.ErrUnsupportedBootstrapFormat) {
		return Rejected{Status: 422, Code: "unsupported_format", Message: "Bootstrap format is not supported"}, nil
	}
	if errors.Is(err, agentprotocol.ErrBootstrapTooLarge) {
		return Rejected{Status: 413, Code: "response_too_large", Message: "Bootstrap response exceeds 16 MiB"}, nil
	}
	validationErr := agentprotocol.ValidateBootstrapBundle(bundle)
	if errors.Is(validationErr, agentprotocol.ErrUnsupportedBootstrapFormat) {
		return Rejected{Status: 422, Code: "unsupported_format", Message: "Bootstrap format is not supported"}, nil
	}
	if errors.Is(validationErr, agentprotocol.ErrBootstrapTooLarge) {
		return Rejected{Status: 413, Code: "response_too_large", Message: "Bootstrap response exceeds 16 MiB"}, nil
	}
	if err != nil || bundle.OperationUID != operation.Spec.OperationID || validationErr != nil {
		return Rejected{Status: 404, Code: "operation_not_found", Message: "Operation or plan was not found"}, nil //nolint:nilerr // Bundle不整合の詳細を外部へ出さない。
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		return nil, fmt.Errorf("marshal bootstrap bundle: %w", err)
	}
	if len(encoded) > agentprotocol.MaxBootstrapBodyBytes {
		return Rejected{Status: 413, Code: "response_too_large", Message: "Bootstrap response exceeds 16 MiB"}, nil
	}
	if err := workflow.sessions.ClaimBootstrap(
		ctx,
		key,
		token,
		string(operation.Spec.HostRef.UID),
		operation.Spec.OperationID,
		workflow.now(),
	); err != nil {
		return Rejected{Status: 401, Code: "unauthorized", Message: "Authentication failed"}, nil //nolint:nilerr // token消費失敗は認証失敗へ正規化する。
	}
	return Accepted{Bundle: bundle}, nil
}

func bearerToken(authorization string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(authorization, prefix) {
		return "", false
	}
	return authorization[len(prefix):], true
}
