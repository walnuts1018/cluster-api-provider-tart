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
	"fmt"
	"time"

	securedelivery "github.com/walnuts1018/cluster-api-provider-tart/domain/agentdelivery/entity/securedelivery"
	sharedresult "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/result"
	sharedworkflow "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/workflow"
	agentprotocol "github.com/walnuts1018/cluster-api-provider-tart/dto/agent"
)

type Result = securedelivery.Result
type Accepted = securedelivery.RegisterAccepted
type Rejected = securedelivery.RegisterRejected

type Command struct {
	OperationUID  string
	Request       agentprotocol.RegisterRequest
	Authorization string
}

type Event interface {
	isEvent()
}

type AgentRegistered struct {
	Response agentprotocol.RegisterResponse
}

type AgentRegistrationRejected struct {
	Status  int
	Code    string
	Message string
}

func (AgentRegistered) isEvent()           {}
func (AgentRegistrationRejected) isEvent() {}

type Workflow struct {
	operations OperationResolver
	verifier   RegistrationVerifier
	sessions   SessionIssuer
	now        func() time.Time
}

func NewWorkflow(
	operations OperationResolver,
	verifier RegistrationVerifier,
	sessions SessionIssuer,
	now func() time.Time,
) *Workflow {
	if now == nil {
		now = time.Now
	}
	return &Workflow{operations: operations, verifier: verifier, sessions: sessions, now: now}
}

func (workflow *Workflow) Do(ctx context.Context, command Command) sharedresult.Result[Event, sharedworkflow.Failure] {
	result, err := workflow.execute(ctx, command)
	if err != nil {
		return sharedworkflow.Failed[Event](sharedworkflow.DependencyFailure{Operation: "register Agent", Detail: err.Error()})
	}
	switch result := result.(type) {
	case Accepted:
		return sharedworkflow.Succeeded[Event](AgentRegistered{Response: result.Response})
	case Rejected:
		return sharedworkflow.Succeeded[Event](AgentRegistrationRejected{Status: result.Status, Code: result.Code, Message: result.Message})
	default:
		panic(fmt.Sprintf("unknown register Agent result: %T", result))
	}
}

func (workflow *Workflow) execute(ctx context.Context, command Command) (Result, error) {
	if command.Request.APIVersion != agentprotocol.APIVersion ||
		command.Request.OperationUID == "" ||
		command.Request.HostUID == "" ||
		command.Request.AgentInstanceID == "" {
		return Rejected{Status: 422, Code: "invalid_request", Message: "Registration request is invalid"}, nil
	}
	if workflow == nil || workflow.operations == nil || workflow.verifier == nil || workflow.sessions == nil {
		return nil, fmt.Errorf("register Agent workflow dependencies are required")
	}
	key, operation, err := workflow.operations.Resolve(ctx, command.OperationUID)
	if err != nil || operation == nil || operation.Spec.OperationID != command.OperationUID {
		return Rejected{Status: 404, Code: "operation_not_found", Message: "Operation or plan was not found"}, nil //nolint:nilerr // 外部へ永続化層の詳細を漏らさずtyped rejectionへ正規化する。
	}
	if err := workflow.verifier.Verify(ctx, operation, command.Authorization, command.Request); err != nil {
		return Rejected{Status: 401, Code: "unauthorized", Message: "Authentication failed"}, nil //nolint:nilerr // 認証失敗はHTTP境界で扱える業務結果へ変換する。
	}
	token, expiresAt, err := workflow.sessions.Issue(ctx, key, command.Request.HostUID, command.Request.OperationUID, workflow.now())
	if err != nil {
		return nil, fmt.Errorf("issue session: %w", err)
	}
	return Accepted{Response: agentprotocol.RegisterResponse{
		APIVersion:    agentprotocol.APIVersion,
		SessionToken:  token.BearerValue(),
		ExpiresAt:     expiresAt,
		PlanDigest:    operation.Spec.PlanDigest,
		AgentSequence: operation.Status.AgentSequence,
	}}, nil
}
