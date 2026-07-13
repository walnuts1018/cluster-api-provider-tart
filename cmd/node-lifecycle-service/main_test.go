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
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"

	distribution "github.com/walnuts1018/cluster-api-provider-tart/internal/application/distributionlifecycle"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
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

type recordingLifecycleProgressReporter struct {
	requests []agentprotocol.NodeLifecycleProgressRequest
}

func (reporter *recordingLifecycleProgressReporter) ReportNodeLifecycleProgress(
	_ context.Context,
	_ string,
	request agentprotocol.NodeLifecycleProgressRequest,
) error {
	reporter.requests = append(reporter.requests, request)
	return nil
}
