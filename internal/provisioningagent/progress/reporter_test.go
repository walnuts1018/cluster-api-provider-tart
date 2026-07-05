package progress

import (
	"context"
	"errors"
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

func TestReporterContinuesFromRegisteredSequence(t *testing.T) {
	client := &recordingClient{}
	reporter, err := New(client, "session", "operation", "sha256:digest", 7)
	if err != nil {
		t.Fatal(err)
	}

	if err := reporter.Report(
		t.Context(),
		agentprotocol.StepWriteImage,
		agentprotocol.DiskRoleOSA,
		10,
		false,
	); err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	if len(client.requests) != 1 || client.requests[0].AgentSequence != 8 {
		t.Fatalf("requests = %#v", client.requests)
	}
}

func TestReporterDoesNotAdvanceSequenceAfterFailure(t *testing.T) {
	reportError := errors.New("unavailable")
	client := &recordingClient{errors: []error{reportError, nil}}
	reporter, err := New(client, "session", "operation", "sha256:digest", 0)
	if err != nil {
		t.Fatal(err)
	}

	if err := reporter.Report(t.Context(), "WriteImage", "", 10, false); !errors.Is(err, reportError) {
		t.Fatalf("first Report() error = %v, want %v", err, reportError)
	}
	if err := reporter.Report(t.Context(), "WriteImage", "", 10, false); err != nil {
		t.Fatalf("second Report() error = %v", err)
	}
	if client.requests[0].AgentSequence != 1 || client.requests[1].AgentSequence != 1 {
		t.Fatalf("sequences = %d, %d, want 1, 1", client.requests[0].AgentSequence, client.requests[1].AgentSequence)
	}
}

type recordingClient struct {
	requests []agentprotocol.ProgressRequest
	errors   []error
}

func (client *recordingClient) ReportProgress(
	_ context.Context,
	_ string,
	request agentprotocol.ProgressRequest,
) (agentprotocol.ProgressResponse, error) {
	client.requests = append(client.requests, request)
	index := len(client.requests) - 1
	if index < len(client.errors) && client.errors[index] != nil {
		return agentprotocol.ProgressResponse{}, client.errors[index]
	}
	return agentprotocol.ProgressResponse{
		APIVersion:    agentprotocol.APIVersion,
		AgentSequence: request.AgentSequence,
	}, nil
}
