package progress

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
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
