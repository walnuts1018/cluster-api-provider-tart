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

package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	nodelifecycle "github.com/walnuts1018/cluster-api-provider-tart/internal/application/nodelifecycle"
	distribution "github.com/walnuts1018/cluster-api-provider-tart/internal/application/nodelifecycleengine"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/nodelifecycleengine"
	agentclient "github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/client"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

func TestParseConfigRequiresNodeLifecycleInputs(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--controller-url=https://controller.test.walnuts.dev",
		"--operation-uid=operation-uid",
		"--session-token-file=/run/tart/session-token",
		"--plan-digest=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"--plan-key-id=test-key",
		"--plan-key-file=/trust/plan.pem",
		"--step=PreflightCompleted",
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.step != domain.StepPreflightCompleted || cfg.planKeyID != "test-key" {
		t.Fatalf("parseConfig() = %#v", cfg)
	}
	if _, err := parseConfig([]string{"--controller-url=https://controller.test.walnuts.dev"}); err == nil {
		t.Fatal("parseConfig() accepted missing identity and trust inputs")
	}
}

func TestParseConfigRejectsUnknownStep(t *testing.T) {
	_, err := parseConfig([]string{
		"--controller-url=https://controller.test.walnuts.dev",
		"--operation-uid=operation-uid",
		"--session-token-file=/run/tart/session-token",
		"--plan-digest=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"--plan-key-id=test-key",
		"--plan-key-file=/trust/plan.pem",
		"--step=RunShell",
	})
	if err == nil {
		t.Fatal("parseConfig() accepted an unknown lifecycle step")
	}
}

func TestLoadPlanPublicKeyAcceptsOnlyEd25519PKIXPEM(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "plan.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadPublicKey(path, "Plan")
	if err != nil {
		t.Fatalf("loadPublicKey() error = %v", err)
	}
	if !publicKey.Equal(got) {
		t.Fatal("loadPublicKey() returned another key")
	}
}

func TestReportStepOutcomeReportsSuccessAndFailure(t *testing.T) {
	cfg := config{
		operationUID: "operation-uid",
		planDigest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		step:         domain.StepPreflightCompleted,
	}
	reporter := &recordingLifecycleProgressReporter{}
	if err := reportStepOutcome(
		t.Context(),
		reporter,
		"session-token",
		cfg,
		distribution.StepResult{},
		nil,
	); err != nil {
		t.Fatalf("reportStepOutcome(success) error = %v", err)
	}
	if reporter.requests[0].Result != agentprotocol.NodeLifecycleResultSucceeded {
		t.Fatalf("success result = %q", reporter.requests[0].Result)
	}

	stepErr := errors.New("step failed")
	if err := reportStepOutcome(
		t.Context(),
		reporter,
		"session-token",
		cfg,
		distribution.StepResult{},
		stepErr,
	); !errors.Is(err, stepErr) {
		t.Fatalf("reportStepOutcome(failure) error = %v, want original step error", err)
	}
	if reporter.requests[1].Result != agentprotocol.NodeLifecycleResultFailed {
		t.Fatalf("failure result = %q", reporter.requests[1].Result)
	}
}

func TestExecuteNodeLifecycleStepUsesPlanDeadlineForStepAndProgress(t *testing.T) {
	deadline := time.Now().Add(24 * time.Hour).UTC()
	plan, err := nodelifecycle.FromDomainPlan(domain.Plan{
		LifecycleRuntime: domain.LifecycleRuntimeKubeadm,
		OperationID:      "operation-uid",
		CurrentVersion:   "v1.35.0",
		TargetVersion:    "v1.36.0",
		UpdateClass:      domain.UpdateClassKubernetesBinary,
		NodeRole:         domain.NodeRoleWorker,
		Steps:            []domain.Step{domain.StepPreflightCompleted},
	}, deadline)
	if err != nil {
		t.Fatalf("FromDomainPlan() error = %v", err)
	}
	cfg := config{
		operationUID: "operation-uid",
		planDigest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		step:         domain.StepPreflightCompleted,
	}
	runner := &recordingLifecycleStepRunner{}
	reporter := &recordingLifecycleProgressReporter{}

	if err := executeNodeLifecycleStep(t.Context(), runner, reporter, "session-token", cfg, plan); err != nil {
		t.Fatalf("executeNodeLifecycleStep() error = %v", err)
	}
	if !runner.deadline.Equal(deadline) {
		t.Fatalf("runner deadline = %s, want %s", runner.deadline, deadline)
	}
	if !reporter.deadline.Equal(deadline) {
		t.Fatalf("reporter deadline = %s, want %s", reporter.deadline, deadline)
	}
}

func TestExecuteNodeLifecycleStepRejectsExpiredPlanDeadline(t *testing.T) {
	deadline := time.Now().Add(-time.Hour).UTC()
	plan, err := nodelifecycle.FromDomainPlan(domain.Plan{
		LifecycleRuntime: domain.LifecycleRuntimeKubeadm,
		OperationID:      "operation-uid",
		CurrentVersion:   "v1.35.0",
		TargetVersion:    "v1.36.0",
		UpdateClass:      domain.UpdateClassKubernetesBinary,
		NodeRole:         domain.NodeRoleWorker,
		Steps:            []domain.Step{domain.StepPreflightCompleted},
	}, deadline)
	if err != nil {
		t.Fatalf("FromDomainPlan() error = %v", err)
	}
	cfg := config{
		operationUID: "operation-uid",
		planDigest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		step:         domain.StepPreflightCompleted,
	}
	runner := &recordingLifecycleStepRunner{}
	reporter := &recordingLifecycleProgressReporter{}

	if err := executeNodeLifecycleStep(t.Context(), runner, reporter, "session-token", cfg, plan); err == nil {
		t.Fatal("executeNodeLifecycleStep() accepted an expired plan deadline")
	}
	if runner.called {
		t.Fatal("RunStep() was called for an expired plan deadline")
	}
	if reporter.calls != 0 {
		t.Fatalf("report attempts = %d, want 0", reporter.calls)
	}
}

func TestReportStepOutcomeRetriesTemporaryErrorUntilSuccess(t *testing.T) {
	cfg := config{
		operationUID: "operation-uid",
		planDigest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		step:         domain.StepPreflightCompleted,
	}
	reporter := &recordingLifecycleProgressReporter{
		reportErrors: []error{
			&agentclient.APIError{StatusCode: http.StatusServiceUnavailable, Code: "TemporaryUnavailable"},
			&url.Error{Op: http.MethodPost, URL: "https://controller.test.walnuts.dev", Err: context.DeadlineExceeded},
		},
	}
	sleepCalls := 0

	err := reportStepOutcomeWithRetry(
		t.Context(),
		reporter,
		"session-token",
		cfg,
		distribution.StepResult{},
		nil,
		func(context.Context, time.Duration) error {
			sleepCalls++
			return nil
		},
	)
	if err != nil {
		t.Fatalf("reportStepOutcomeWithRetry() error = %v", err)
	}
	if reporter.calls != 3 {
		t.Fatalf("report attempts = %d, want 3", reporter.calls)
	}
	if sleepCalls != 2 {
		t.Fatalf("sleep calls = %d, want 2", sleepCalls)
	}
}

func TestReportStepOutcomeFailsImmediatelyOnNonRetryableAPIError(t *testing.T) {
	cfg := config{
		operationUID: "operation-uid",
		planDigest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		step:         domain.StepPreflightCompleted,
	}
	reporter := &recordingLifecycleProgressReporter{
		reportErrors: []error{
			&agentclient.APIError{StatusCode: http.StatusUnauthorized, Code: "Unauthorized"},
		},
	}
	sleepCalls := 0

	err := reportStepOutcomeWithRetry(
		t.Context(),
		reporter,
		"session-token",
		cfg,
		distribution.StepResult{},
		nil,
		func(context.Context, time.Duration) error {
			sleepCalls++
			return nil
		},
	)
	var apiErr *agentclient.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reportStepOutcomeWithRetry() error = %v, want unauthorized API error", err)
	}
	if reporter.calls != 1 {
		t.Fatalf("report attempts = %d, want 1", reporter.calls)
	}
	if sleepCalls != 0 {
		t.Fatalf("sleep calls = %d, want 0", sleepCalls)
	}
}

func TestFetchNodeLifecyclePlanWithRetryRecoversAfterInnerRetriesExhausted(t *testing.T) {
	deadline := time.Now().Add(24 * time.Hour).UTC()
	expectedPlan, err := nodelifecycle.FromDomainPlan(domain.Plan{
		LifecycleRuntime: domain.LifecycleRuntimeKubeadm,
		OperationID:      "operation-uid",
		CurrentVersion:   "v1.35.0",
		TargetVersion:    "v1.36.0",
		UpdateClass:      domain.UpdateClassKubernetesBinary,
		NodeRole:         domain.NodeRoleWorker,
		Steps:            []domain.Step{domain.StepPreflightCompleted},
	}, deadline)
	if err != nil {
		t.Fatalf("FromDomainPlan() error = %v", err)
	}
	signedPlan, publicKey := mustSignNodeLifecyclePlan(t, expectedPlan)
	const sessionToken = "session-token"
	const expectedPath = "/v1/operations/operation-uid/node-lifecycle-plan"

	var (
		mu             sync.Mutex
		requestCount   int
		paths          []string
		authorizations []string
	)
	server := newLocalTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		paths = append(paths, r.URL.Path)
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		currentAttempt := requestCount
		mu.Unlock()

		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want %q", r.Method, http.MethodGet)
		}
		if r.URL.Path != expectedPath {
			t.Errorf("path = %q, want %q", r.URL.Path, expectedPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+sessionToken {
			t.Errorf("Authorization = %q, want %q", got, "Bearer "+sessionToken)
		}
		if currentAttempt <= 3 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			if _, err := w.Write([]byte(`{"code":"TemporaryUnavailable"}`)); err != nil {
				t.Errorf("write error response: %v", err)
			}
			return
		}
		writeJSONResponse(t, w, signedPlan)
	}))
	defer server.Close()

	fetcher, err := agentclient.New(agentclient.Config{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		TrustStore: agentprotocol.StaticTrustStore{"test-key": publicKey},
		RetryDelay: func(uint) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatalf("agentclient.New() error = %v", err)
	}
	sleepCalls := 0

	plan, err := fetchNodeLifecyclePlanWithRetry(
		t.Context(),
		fetcher,
		"operation-uid",
		sessionToken,
		mustNodeLifecyclePlanDigest(t, expectedPlan),
		func(context.Context, time.Duration) error {
			sleepCalls++
			return nil
		},
	)
	if err != nil {
		t.Fatalf("fetchNodeLifecyclePlanWithRetry() error = %v", err)
	}
	if sleepCalls != 1 {
		t.Fatalf("sleep calls = %d, want 1", sleepCalls)
	}
	if plan.Value().OperationID != expectedPlan.Value().OperationID {
		t.Fatalf("operation id = %q, want %q", plan.Value().OperationID, expectedPlan.Value().OperationID)
	}
	mu.Lock()
	defer mu.Unlock()
	if requestCount != 4 {
		t.Fatalf("request count = %d, want 4", requestCount)
	}
	for i, path := range paths {
		if path != expectedPath {
			t.Fatalf("paths[%d] = %q, want %q", i, path, expectedPath)
		}
	}
	for i, authorization := range authorizations {
		if authorization != "Bearer "+sessionToken {
			t.Fatalf("authorizations[%d] = %q, want %q", i, authorization, "Bearer "+sessionToken)
		}
	}
}

func TestFetchNodeLifecyclePlanWithRetryFailsImmediatelyOnNonRetryableAPIError(t *testing.T) {
	fetcher := &recordingLifecyclePlanFetcher{
		errors: []error{
			&agentclient.APIError{StatusCode: http.StatusUnauthorized, Code: "Unauthorized"},
		},
	}
	sleepCalls := 0

	_, err := fetchNodeLifecyclePlanWithRetry(
		t.Context(),
		fetcher,
		"operation-uid",
		"session-token",
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		func(context.Context, time.Duration) error {
			sleepCalls++
			return nil
		},
	)
	var apiErr *agentclient.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("fetchNodeLifecyclePlanWithRetry() error = %v, want unauthorized API error", err)
	}
	if fetcher.calls != 1 {
		t.Fatalf("fetch attempts = %d, want 1", fetcher.calls)
	}
	if sleepCalls != 0 {
		t.Fatalf("sleep calls = %d, want 0", sleepCalls)
	}
}

func TestNodeLifecycleServiceRecoversTemporaryOutageAcrossPlanFetchAndProgressReport(t *testing.T) {
	deadline := time.Date(2030, 7, 16, 12, 0, 0, 0, time.UTC)
	expectedPlan, err := nodelifecycle.FromDomainPlan(domain.Plan{
		LifecycleRuntime: domain.LifecycleRuntimeKubeadm,
		OperationID:      "operation-uid",
		CurrentVersion:   "v1.35.0",
		TargetVersion:    "v1.36.0",
		UpdateClass:      domain.UpdateClassKubernetesBinary,
		NodeRole:         domain.NodeRoleWorker,
		Steps:            []domain.Step{domain.StepPreflightCompleted},
	}, deadline)
	if err != nil {
		t.Fatalf("FromDomainPlan() error = %v", err)
	}
	signedPlan, publicKey := mustSignNodeLifecyclePlan(t, expectedPlan)
	const sessionToken = "session-token"
	const planPath = "/v1/operations/operation-uid/node-lifecycle-plan"
	const progressPath = "/v1/operations/operation-uid/node-lifecycle-progress"

	var (
		mu                   sync.Mutex
		planRequestCount     int
		progressRequestCount int
		paths                []string
		authorizations       []string
	)
	server := newLocalTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case planPath:
			planRequestCount++
			currentAttempt := planRequestCount
			mu.Unlock()
			if currentAttempt <= 3 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				if _, err := w.Write([]byte(`{"code":"TemporaryUnavailable"}`)); err != nil {
					t.Errorf("write plan error response: %v", err)
				}
				return
			}
			writeJSONResponse(t, w, signedPlan)
		case progressPath:
			progressRequestCount++
			currentAttempt := progressRequestCount
			mu.Unlock()
			if currentAttempt <= 3 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				if _, err := w.Write([]byte(`{"code":"TemporaryUnavailable"}`)); err != nil {
					t.Errorf("write progress error response: %v", err)
				}
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			mu.Unlock()
			t.Errorf("unexpected path = %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	apiClient, err := agentclient.New(agentclient.Config{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		TrustStore: agentprotocol.StaticTrustStore{"test-key": publicKey},
		RetryDelay: func(uint) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatalf("agentclient.New() error = %v", err)
	}

	fetchSleepCalls := 0
	plan, err := fetchNodeLifecyclePlanWithRetry(
		t.Context(),
		apiClient,
		"operation-uid",
		sessionToken,
		mustNodeLifecyclePlanDigest(t, expectedPlan),
		func(context.Context, time.Duration) error {
			fetchSleepCalls++
			return nil
		},
	)
	if err != nil {
		t.Fatalf("fetchNodeLifecyclePlanWithRetry() error = %v", err)
	}

	progressSleepCalls := 0
	err = reportStepOutcomeWithRetry(
		t.Context(),
		apiClient,
		sessionToken,
		config{
			operationUID: "operation-uid",
			planDigest:   mustNodeLifecyclePlanDigest(t, plan),
			step:         domain.StepPreflightCompleted,
		},
		distribution.StepResult{},
		nil,
		func(context.Context, time.Duration) error {
			progressSleepCalls++
			return nil
		},
	)
	if err != nil {
		t.Fatalf("reportStepOutcomeWithRetry() error = %v", err)
	}

	if fetchSleepCalls != 1 {
		t.Fatalf("fetch sleep calls = %d, want 1", fetchSleepCalls)
	}
	if progressSleepCalls != 1 {
		t.Fatalf("progress sleep calls = %d, want 1", progressSleepCalls)
	}

	mu.Lock()
	defer mu.Unlock()
	if planRequestCount != 4 {
		t.Fatalf("plan request count = %d, want 4", planRequestCount)
	}
	if progressRequestCount != 4 {
		t.Fatalf("progress request count = %d, want 4", progressRequestCount)
	}
	if len(paths) != 8 {
		t.Fatalf("paths length = %d, want 8", len(paths))
	}
	for i := range 4 {
		if paths[i] != planPath {
			t.Fatalf("paths[%d] = %q, want %q", i, paths[i], planPath)
		}
	}
	for i := 4; i < 8; i++ {
		if paths[i] != progressPath {
			t.Fatalf("paths[%d] = %q, want %q", i, paths[i], progressPath)
		}
	}
	for i, authorization := range authorizations {
		if authorization != "Bearer "+sessionToken {
			t.Fatalf("authorizations[%d] = %q, want %q", i, authorization, "Bearer "+sessionToken)
		}
	}
}

func TestReportStepOutcomeWithRetryRecoversAfterInnerRetriesExhausted(t *testing.T) {
	cfg := config{
		operationUID: "operation-uid",
		planDigest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		step:         domain.StepPreflightCompleted,
	}
	const sessionToken = "session-token"
	const expectedPath = "/v1/operations/operation-uid/node-lifecycle-progress"

	var (
		mu             sync.Mutex
		requestCount   int
		paths          []string
		authorizations []string
	)
	server := newLocalTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		paths = append(paths, r.URL.Path)
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		currentAttempt := requestCount
		mu.Unlock()

		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want %q", r.Method, http.MethodPost)
		}
		if r.URL.Path != expectedPath {
			t.Errorf("path = %q, want %q", r.URL.Path, expectedPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+sessionToken {
			t.Errorf("Authorization = %q, want %q", got, "Bearer "+sessionToken)
		}
		if currentAttempt <= 3 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			if _, err := w.Write([]byte(`{"code":"TemporaryUnavailable"}`)); err != nil {
				t.Errorf("write error response: %v", err)
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	reporter, err := agentclient.New(agentclient.Config{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		TrustStore: agentprotocol.StaticTrustStore{"test-key": publicKey},
		RetryDelay: func(uint) time.Duration { return 0 },
	})
	if err != nil {
		t.Fatalf("agentclient.New() error = %v", err)
	}

	err = reportStepOutcomeWithRetry(
		t.Context(),
		reporter,
		sessionToken,
		cfg,
		distribution.StepResult{},
		nil,
		func(context.Context, time.Duration) error {
			return nil
		},
	)
	if err != nil {
		t.Fatalf("reportStepOutcomeWithRetry() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if requestCount != 4 {
		t.Fatalf("request count = %d, want 4", requestCount)
	}
	for i, path := range paths {
		if path != expectedPath {
			t.Fatalf("paths[%d] = %q, want %q", i, path, expectedPath)
		}
	}
	for i, authorization := range authorizations {
		if authorization != "Bearer "+sessionToken {
			t.Fatalf("authorizations[%d] = %q, want %q", i, authorization, "Bearer "+sessionToken)
		}
	}
}

func TestReportStepOutcomeStopsAtDeadline(t *testing.T) {
	cfg := config{
		operationUID: "operation-uid",
		planDigest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		step:         domain.StepPreflightCompleted,
	}
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	reporter := &recordingLifecycleProgressReporter{
		reportErrors: []error{
			&agentclient.APIError{StatusCode: http.StatusServiceUnavailable, Code: "TemporaryUnavailable"},
		},
	}
	sleepCalls := 0

	err := reportStepOutcomeWithRetry(
		ctx,
		reporter,
		"session-token",
		cfg,
		distribution.StepResult{},
		nil,
		func(context.Context, time.Duration) error {
			sleepCalls++
			return nil
		},
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("reportStepOutcomeWithRetry() error = %v, want deadline exceeded", err)
	}
	if reporter.calls != 1 {
		t.Fatalf("report attempts = %d, want 1", reporter.calls)
	}
	if sleepCalls != 0 {
		t.Fatalf("sleep calls = %d, want 0", sleepCalls)
	}
}

type recordingLifecycleProgressReporter struct {
	requests     []agentprotocol.NodeLifecycleProgressRequest
	reportErrors []error
	deadline     time.Time
	calls        int
}

func (reporter *recordingLifecycleProgressReporter) ReportNodeLifecycleProgress(
	ctx context.Context,
	_ string,
	request agentprotocol.NodeLifecycleProgressRequest,
) error {
	reporter.calls++
	if deadline, ok := ctx.Deadline(); ok {
		reporter.deadline = deadline
	}
	reporter.requests = append(reporter.requests, request)
	if len(reporter.reportErrors) == 0 {
		return nil
	}
	err := reporter.reportErrors[0]
	reporter.reportErrors = reporter.reportErrors[1:]
	if err == nil {
		return nil
	}
	return err
}

type recordingLifecyclePlanFetcher struct {
	plans  []nodelifecycle.ValidatedPlan
	errors []error
	calls  int
}

func (fetcher *recordingLifecyclePlanFetcher) FetchNodeLifecyclePlan(
	_ context.Context,
	_, _, _ string,
) (nodelifecycle.ValidatedPlan, error) {
	fetcher.calls++
	if len(fetcher.errors) != 0 {
		err := fetcher.errors[0]
		fetcher.errors = fetcher.errors[1:]
		if err != nil {
			return nodelifecycle.ValidatedPlan{}, err
		}
	}
	if len(fetcher.plans) == 0 {
		return nodelifecycle.ValidatedPlan{}, errors.New("unexpected FetchNodeLifecyclePlan call")
	}
	plan := fetcher.plans[0]
	fetcher.plans = fetcher.plans[1:]
	return plan, nil
}

type recordingLifecycleStepRunner struct {
	deadline time.Time
	called   bool
}

func (runner *recordingLifecycleStepRunner) RunStep(
	ctx context.Context,
	_ domain.Plan,
	_ domain.Step,
) (distribution.StepResult, error) {
	runner.called = true
	if deadline, ok := ctx.Deadline(); ok {
		runner.deadline = deadline
	}
	return distribution.StepResult{}, nil
}

func newLocalTLSServer(t *testing.T, handler http.Handler) (server *httptest.Server) {
	t.Helper()
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	server = httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.StartTLS()
	return server
}

func mustNodeLifecyclePlanDigest(t *testing.T, plan nodelifecycle.ValidatedPlan) string {
	t.Helper()
	digest, err := plan.Digest()
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	return digest.String()
}

func mustSignNodeLifecyclePlan(
	t *testing.T,
	plan nodelifecycle.ValidatedPlan,
) (nodelifecycle.SignedPlan, ed25519.PublicKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	signature, err := nodelifecycle.Sign(plan, "test-key", privateKey)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	return nodelifecycle.SignedPlan{
		Plan:      plan.Value(),
		Signature: signature,
	}, publicKey
}

func writeJSONResponse(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Errorf("Encode() error = %v", err)
	}
}
