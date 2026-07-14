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
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	infrastructurev1beta1 "github.com/walnuts1018/cluster-api-provider-tart/api/v1beta1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Workflow struct {
	ports Ports
}

func NewWorkflow(ports Ports) *Workflow {
	if ports.Now == nil {
		ports.Now = time.Now
	}
	return &Workflow{ports: ports}
}

type RegisterInput struct {
	OperationUID  string
	Request       agentprotocol.RegisterRequest
	Authorization string
}

func (workflow *Workflow) Register(ctx context.Context, input RegisterInput) (Result, error) {
	if input.Request.APIVersion != agentprotocol.APIVersion ||
		input.Request.OperationUID == "" ||
		input.Request.HostUID == "" ||
		input.Request.AgentInstanceID == "" {
		return RegisterRejected{Status: 422, Code: "invalid_request", Message: "Registration request is invalid"}, nil
	}
	if workflow == nil || workflow.ports.Operations == nil || workflow.ports.RegistrationVerifier == nil || workflow.ports.Sessions == nil {
		return nil, fmt.Errorf("secure delivery workflow ports are required")
	}
	key, operation, err := workflow.ports.Operations.Resolve(ctx, input.OperationUID)
	if err != nil || operation == nil || operation.Spec.OperationID != input.OperationUID {
		return RegisterRejected{Status: 404, Code: "operation_not_found", Message: "Operation or plan was not found"}, nil //nolint:nilerr // HTTP境界では未検出Operationをtyped rejectionへ正規化する。
	}
	if err := workflow.ports.RegistrationVerifier.Verify(ctx, operation, input.Authorization, input.Request); err != nil {
		return RegisterRejected{Status: 401, Code: "unauthorized", Message: "Authentication failed"}, nil //nolint:nilerr // 認証失敗はHTTP 401 resultで返す業務失敗。
	}
	token, expiresAt, err := workflow.ports.Sessions.Issue(
		ctx,
		key,
		input.Request.HostUID,
		input.Request.OperationUID,
		workflow.ports.Now(),
	)
	if err != nil {
		return nil, fmt.Errorf("issue session: %w", err)
	}
	return RegisterAccepted{Response: agentprotocol.RegisterResponse{
		APIVersion:    agentprotocol.APIVersion,
		SessionToken:  token.BearerValue(),
		ExpiresAt:     expiresAt,
		PlanDigest:    operation.Spec.PlanDigest,
		AgentSequence: operation.Status.AgentSequence,
	}}, nil
}

type BootstrapInput struct {
	OperationUID  string
	Authorization string
}

func (workflow *Workflow) Bootstrap(ctx context.Context, input BootstrapInput) (Result, error) {
	if workflow == nil || workflow.ports.Operations == nil || workflow.ports.Sessions == nil || workflow.ports.Bootstrap == nil {
		return nil, fmt.Errorf("secure delivery workflow ports are required")
	}
	key, operation, token, ok := workflow.operationAndToken(ctx, input)
	if !ok {
		return BootstrapRejected{Status: 401, Code: "unauthorized", Message: "Authentication failed"}, nil
	}
	bundle, err := workflow.ports.Bootstrap.GetBootstrapBundle(ctx, key)
	if errors.Is(err, agentprotocol.ErrUnsupportedBootstrapFormat) {
		return BootstrapRejected{Status: 422, Code: "unsupported_format", Message: "Bootstrap format is not supported"}, nil
	}
	if errors.Is(err, agentprotocol.ErrBootstrapTooLarge) {
		return BootstrapRejected{Status: 413, Code: "response_too_large", Message: "Bootstrap response exceeds 16 MiB"}, nil
	}
	validationErr := agentprotocol.ValidateBootstrapBundle(bundle)
	if errors.Is(validationErr, agentprotocol.ErrUnsupportedBootstrapFormat) {
		return BootstrapRejected{Status: 422, Code: "unsupported_format", Message: "Bootstrap format is not supported"}, nil
	}
	if errors.Is(validationErr, agentprotocol.ErrBootstrapTooLarge) {
		return BootstrapRejected{Status: 413, Code: "response_too_large", Message: "Bootstrap response exceeds 16 MiB"}, nil
	}
	if err != nil || bundle.OperationUID != operation.Spec.OperationID || validationErr != nil {
		return BootstrapRejected{Status: 404, Code: "operation_not_found", Message: "Operation or plan was not found"}, nil //nolint:nilerr // Bundle不整合は外部へ詳細を出さずtyped rejectionへ正規化する。
	}
	encoded, err := json.Marshal(bundle)
	if err != nil {
		return nil, fmt.Errorf("marshal bootstrap bundle: %w", err)
	}
	if len(encoded) > agentprotocol.MaxBootstrapBodyBytes {
		return BootstrapRejected{Status: 413, Code: "response_too_large", Message: "Bootstrap response exceeds 16 MiB"}, nil
	}
	if err := workflow.ports.Sessions.ClaimBootstrap(
		ctx,
		key,
		token,
		string(operation.Spec.HostRef.UID),
		operation.Spec.OperationID,
		workflow.ports.Now(),
	); err != nil {
		return BootstrapRejected{Status: 401, Code: "unauthorized", Message: "Authentication failed"}, nil //nolint:nilerr // token消費失敗はHTTP 401 resultで返す業務失敗。
	}
	return BootstrapAccepted{Bundle: bundle}, nil
}

func (workflow *Workflow) operationAndToken(
	ctx context.Context,
	input BootstrapInput,
) (key client.ObjectKey, operation *infrastructurev1beta1.TartHostOperation, token string, ok bool) {
	key, operation, err := workflow.ports.Operations.Resolve(ctx, input.OperationUID)
	if err != nil || operation == nil || operation.Spec.OperationID != input.OperationUID {
		return client.ObjectKey{}, nil, "", false
	}
	bearer, ok := bearerToken(input.Authorization)
	if !ok {
		return client.ObjectKey{}, nil, "", false
	}
	return key, operation, bearer, true
}

func bearerToken(authorization string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(authorization, prefix) {
		return "", false
	}
	return authorization[len(prefix):], true
}
