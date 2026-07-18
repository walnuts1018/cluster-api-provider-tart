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

package progress

import (
	"context"
	"errors"
	"fmt"
	"sync"

	agentprotocol "github.com/walnuts1018/cluster-api-provider-tart/dto/agent"
)

type Client interface {
	ReportProgress(
		context.Context,
		string,
		agentprotocol.ProgressRequest,
	) (agentprotocol.ProgressResponse, error)
}

type Reporter struct {
	client       Client
	sessionToken string
	operationUID string
	planDigest   string

	mu       sync.Mutex
	sequence int64
}

func New(
	client Client,
	sessionToken, operationUID, planDigest string,
	initialSequence int64,
) (*Reporter, error) {
	if client == nil {
		return nil, errors.New("progress client is required")
	}
	if sessionToken == "" || operationUID == "" || planDigest == "" || initialSequence < 0 {
		return nil, errors.New("progress reporter configuration is invalid")
	}
	return &Reporter{
		client:       client,
		sessionToken: sessionToken,
		operationUID: operationUID,
		planDigest:   planDigest,
		sequence:     initialSequence,
	}, nil
}

func (reporter *Reporter) Report(
	ctx context.Context,
	step string,
	role agentprotocol.DiskRole,
	percent int32,
	completed bool,
) error {
	reporter.mu.Lock()
	defer reporter.mu.Unlock()

	nextSequence := reporter.sequence + 1
	response, err := reporter.client.ReportProgress(ctx, reporter.sessionToken, agentprotocol.ProgressRequest{
		APIVersion:    agentprotocol.APIVersion,
		OperationUID:  reporter.operationUID,
		PlanDigest:    reporter.planDigest,
		AgentSequence: nextSequence,
		Step:          step,
		DiskRole:      role,
		Percent:       percent,
		Completed:     completed,
	})
	if err != nil {
		return fmt.Errorf("send progress report: %w", err)
	}
	if response.AgentSequence != nextSequence {
		return fmt.Errorf(
			"progress response sequence is %d, expected %d",
			response.AgentSequence,
			nextSequence,
		)
	}
	reporter.sequence = response.AgentSequence
	return nil
}
