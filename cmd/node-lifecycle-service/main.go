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
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"

	kubeadmadapter "github.com/walnuts1018/cluster-api-provider-tart/internal/adapter/kubeadm"
	distribution "github.com/walnuts1018/cluster-api-provider-tart/internal/application/distributionlifecycle"
	domain "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/distributionlifecycle"
	agentclient "github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/client"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

type config struct {
	controllerURL    string
	operationUID     string
	sessionTokenFile string
	planDigest       string
	tlsCAFile        string
	planKeyID        string
	planKeyFile      string
	step             domain.Step
	kubeadmPath      string
	etcdctlPath      string
	kubectlPath      string
	snapshotDir      string
	nodeName         string
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		slog.Error("Node Lifecycle Service failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	publicKey, err := loadPublicKey(cfg.planKeyFile, "Plan")
	if err != nil {
		return err
	}
	sessionTokenBytes, err := os.ReadFile(cfg.sessionTokenFile)
	if err != nil {
		return fmt.Errorf("read session token: %w", err)
	}
	sessionToken := strings.TrimSpace(string(sessionTokenBytes))
	if sessionToken == "" {
		return errors.New("session token file is empty")
	}
	httpClient, err := newHTTPClient(cfg.tlsCAFile)
	if err != nil {
		return err
	}
	apiClient, err := agentclient.New(agentclient.Config{
		BaseURL:    cfg.controllerURL,
		HTTPClient: httpClient,
		TrustStore: agentprotocol.StaticTrustStore{cfg.planKeyID: publicKey},
	})
	if err != nil {
		return err
	}
	plan, err := apiClient.FetchNodeLifecyclePlan(ctx, cfg.operationUID, sessionToken, cfg.planDigest)
	if err != nil {
		return err
	}
	domainPlan, err := plan.DomainPlan()
	if err != nil {
		return err
	}
	runtime := kubeadmadapter.NewRuntime(kubeadmadapter.RuntimeConfig{
		KubeadmPath: cfg.kubeadmPath,
		EtcdctlPath: cfg.etcdctlPath,
		KubectlPath: cfg.kubectlPath,
		SnapshotDir: cfg.snapshotDir,
		NodeName:    cfg.nodeName,
	})
	service := distribution.NewService(kubeadmadapter.NewDriver(runtime))
	result, stepErr := service.RunStep(ctx, domainPlan, cfg.step)
	if err := reportStepOutcome(ctx, apiClient, sessionToken, cfg, result, stepErr); err != nil {
		return err
	}
	if stepErr != nil {
		return stepErr
	}
	slog.Info("Node Lifecycle step completed", "operation_uid", cfg.operationUID, "step", cfg.step)
	return nil
}

type lifecycleProgressReporter interface {
	ReportNodeLifecycleProgress(context.Context, string, agentprotocol.NodeLifecycleProgressRequest) error
}

func reportStepOutcome(
	ctx context.Context,
	reporter lifecycleProgressReporter,
	sessionToken string,
	cfg config,
	result distribution.StepResult,
	stepErr error,
) error {
	report := agentprotocol.NodeLifecycleProgressRequest{
		APIVersion:   agentprotocol.APIVersion,
		OperationUID: cfg.operationUID,
		PlanDigest:   cfg.planDigest,
		Step:         string(cfg.step),
		Result:       agentprotocol.NodeLifecycleResultSucceeded,
		SnapshotRef:  result.SnapshotRef,
	}
	if stepErr != nil {
		report.Result = agentprotocol.NodeLifecycleResultFailed
	}
	if err := reporter.ReportNodeLifecycleProgress(ctx, sessionToken, report); err != nil {
		if stepErr != nil {
			return errors.Join(stepErr, fmt.Errorf("report Node Lifecycle step failure: %w", err))
		}
		return fmt.Errorf("report Node Lifecycle step success: %w", err)
	}
	if stepErr != nil {
		return stepErr
	}
	return nil
}

func parseConfig(args []string) (config, error) {
	var cfg config
	var step string
	flags := flag.NewFlagSet("node-lifecycle-service", flag.ContinueOnError)
	flags.StringVar(&cfg.controllerURL, "controller-url", "", "Agent API HTTPS base URL.")
	flags.StringVar(&cfg.operationUID, "operation-uid", "", "Operation UID assigned by the controller.")
	flags.StringVar(&cfg.sessionTokenFile, "session-token-file", "", "File containing the Agent API session token.")
	flags.StringVar(&cfg.planDigest, "plan-digest", "", "Expected Node Lifecycle Plan digest.")
	flags.StringVar(&cfg.tlsCAFile, "tls-ca-file", "", "PEM CA bundle used to verify the Agent API.")
	flags.StringVar(&cfg.planKeyID, "plan-key-id", "", "Trusted Plan signing key ID.")
	flags.StringVar(&cfg.planKeyFile, "plan-key-file", "", "PEM Ed25519 public key used to verify Plans.")
	flags.StringVar(&step, "step", "", "Lifecycle step to execute.")
	flags.StringVar(&cfg.kubeadmPath, "kubeadm-path", "kubeadm", "Path to kubeadm.")
	flags.StringVar(&cfg.etcdctlPath, "etcdctl-path", "etcdctl", "Path to etcdctl.")
	flags.StringVar(&cfg.kubectlPath, "kubectl-path", "kubectl", "Path to kubectl.")
	flags.StringVar(&cfg.snapshotDir, "snapshot-dir", "/var/lib/tart/snapshots", "Directory for etcd snapshots.")
	flags.StringVar(&cfg.nodeName, "node-name", "", "Kubernetes Node name used by health verification.")
	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	if flags.NArg() != 0 {
		return config{}, errors.New("unexpected positional arguments")
	}
	required := map[string]string{
		"controller-url":     cfg.controllerURL,
		"operation-uid":      cfg.operationUID,
		"session-token-file": cfg.sessionTokenFile,
		"plan-digest":        cfg.planDigest,
		"plan-key-id":        cfg.planKeyID,
		"plan-key-file":      cfg.planKeyFile,
		"step":               step,
	}
	for name, value := range required {
		if value == "" {
			return config{}, fmt.Errorf("--%s is required", name)
		}
	}
	parsedStep, err := parseStep(step)
	if err != nil {
		return config{}, err
	}
	cfg.step = parsedStep
	return cfg, nil
}

func parseStep(value string) (domain.Step, error) {
	step := domain.Step(value)
	if slices.Contains(domain.LifecycleSteps(), step) {
		return step, nil
	}
	return "", fmt.Errorf("unknown lifecycle step %q", value)
}

func loadPublicKey(path, purpose string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s public key: %w", purpose, err)
	}
	block, rest := pem.Decode(data)
	if block == nil || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, fmt.Errorf("%s public key file must contain exactly one PEM block", purpose)
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse %s public key: %w", purpose, err)
	}
	publicKey, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%s public key must be Ed25519", purpose)
	}
	return publicKey, nil
}

func newHTTPClient(caFile string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13}
	if caFile != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system CA pool: %w", err)
		}
		data, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read Agent API CA bundle: %w", err)
		}
		if !roots.AppendCertsFromPEM(data) {
			return nil, errors.New("agent API CA bundle contains no certificates")
		}
		tlsConfig.RootCAs = roots
	}
	transport.TLSClientConfig = tlsConfig
	return &http.Client{Transport: transport}, nil
}
