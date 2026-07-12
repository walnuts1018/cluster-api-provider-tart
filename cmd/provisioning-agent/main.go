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
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"

	provisioningagent "github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/artifactfetch"
	agentbootstrap "github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/bootstrap"
	boottrial "github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/boottrial"
	agentclient "github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/client"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/disk"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/inventory"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/layout"
	agentplan "github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/plan"
	agentprogress "github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/progress"
	agentwriter "github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/writer"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/registrycredential"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
)

const defaultSystemUUIDPath = "/sys/class/dmi/id/product_uuid"

type config struct {
	controllerURL      string
	operationUID       string
	hostUID            string
	systemUUID         string
	bootMAC            string
	tlsCAFile          string
	planKeyID          string
	planKeyFile        string
	artifactKeyID      string
	artifactKeyFile    string
	registryConfig     string
	bootTrialDriver    string
	preflight          bool
	prepareLayout      bool
	writePayloads      bool
	applyBootstrap     bool
	reportBoot         bool
	stateDir           string
	bootstrapWorkDir   string
	bootstrapAdapter   string
	bootID             string
	activeSlot         string
	artifactGeneration uint64
	stateMounted       bool
	dataMounted        bool
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		slog.Error("Provisioning Agent failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	systemUUID := cfg.systemUUID
	if systemUUID == "" {
		value, err := os.ReadFile(defaultSystemUUIDPath)
		if err != nil {
			return fmt.Errorf("read system UUID: %w", err)
		}
		systemUUID = strings.TrimSpace(string(value))
	}
	publicKey, err := loadPublicKey(cfg.planKeyFile, "Plan")
	if err != nil {
		return err
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
	devices, err := inventory.NewLinuxCollector(inventory.DefaultLinuxPaths()).Collect()
	if err != nil {
		return fmt.Errorf("collect Linux disk inventory: %w", err)
	}
	registration, err := apiClient.Register(ctx, agentprotocol.RegisterRequest{
		APIVersion:      agentprotocol.APIVersion,
		OperationUID:    cfg.operationUID,
		HostUID:         cfg.hostUID,
		AgentInstanceID: uuid.NewString(),
		Inventory:       inventory.ToProtocol(systemUUID, cfg.bootMAC, devices),
	})
	if err != nil {
		return err
	}
	validatedPlan, err := apiClient.FetchPlan(
		ctx,
		cfg.operationUID,
		registration.SessionToken,
		registration.PlanDigest,
	)
	if err != nil {
		return err
	}
	if !time.Now().Before(validatedPlan.Value().Deadline) {
		return errors.New("plan deadline has expired")
	}
	// 破壊的処理とRegistry通信が署名済みPlanの期限を越えて継続しないよう、以後のI/Oへdeadlineを伝播する。
	operationContext, cancelOperation := context.WithDeadline(ctx, validatedPlan.Value().Deadline)
	defer cancelOperation()
	if err := agentplan.ValidateTargets(validatedPlan); err != nil {
		return fmt.Errorf("validate Plan targets: %w", err)
	}
	target, err := disk.Select(validatedPlan.Value().RootDevice, devices)
	if err != nil {
		return fmt.Errorf("select root disk: %w", err)
	}
	if cfg.prepareLayout {
		// ProvisionではGPTを作り直す破壊操作なので、署名済みPlanとdisk identity検証後だけ実行する。
		resolved, err := layout.NewManager(layout.NewLinuxDiskIO()).Prepare(
			operationContext,
			validatedPlan.Value().OperationType,
			target,
		)
		if err != nil {
			return fmt.Errorf("prepare amd64 UEFI A/B layout: %w", err)
		}
		slog.Info(
			"Provisioning Agent partition layout prepared",
			"operation_uid", cfg.operationUID,
			"target_device", target.Path,
			"platform_profile", layout.ProfileID,
			"role_count", len(resolved),
		)
		// TODO: boot trial metadata更新後、このlayout専用診断を通常Agent実行へ統合する。
		return nil
	}
	if cfg.writePayloads {
		artifactPublicKey, err := loadPublicKey(cfg.artifactKeyFile, "artifact")
		if err != nil {
			return err
		}
		credential, err := registrycredential.Load(cfg.registryConfig)
		if err != nil {
			return err
		}
		source, err := artifactfetch.NewOCI(
			artifact.StaticTrustStore{cfg.artifactKeyID: artifactPublicKey},
			credential,
		)
		if err != nil {
			return err
		}
		progressReporter, err := agentprogress.New(
			apiClient,
			registration.SessionToken,
			cfg.operationUID,
			registration.PlanDigest,
			registration.AgentSequence,
		)
		if err != nil {
			return err
		}
		targetWriter := agentwriter.NewWithSanitizer(
			layout.NewManager(layout.NewLinuxDiskIO()),
			source,
			agentwriter.LinuxDeviceOpener{},
			agentwriter.NewLinuxSanitizer(),
			func(ctx context.Context, progress agentwriter.Progress) error {
				slog.Info(
					"Provisioning Agent payload write progress",
					"operation_uid", cfg.operationUID,
					"step", progress.Step,
					"role", progress.DiskRole,
					"percent", progress.Percent,
					"completed", progress.Completed,
				)
				return progressReporter.Report(
					ctx,
					progress.Step,
					progress.DiskRole,
					progress.Percent,
					progress.Completed,
				)
			},
		)
		if cfg.bootTrialDriver != "" {
			targetWriter.SetBootTrialDriver(boottrial.NewCommandDriver(cfg.bootTrialDriver, nil))
		}
		if err := provisioningagent.NewService(targetWriter).Execute(operationContext, validatedPlan, devices); err != nil {
			return err
		}
		attributes := []any{
			"operation_uid", cfg.operationUID,
			"target_device", target.Path,
		}
		if validatedPlan.Value().Artifact != nil {
			attributes = append(attributes, "artifact_generation", validatedPlan.Value().Artifact.Generation)
		}
		slog.Info("Provisioning Agent payloads written and verified", attributes...)
		// TODO: boot試行から再起動までを含む通常Agent実行へ昇格する条件が揃うまで、書込み診断を維持する。
		return nil
	}
	if cfg.applyBootstrap {
		if validatedPlan.Value().Bootstrap == nil {
			slog.Info("Provisioning Agent bootstrap apply skipped because Plan has no bootstrap target", "operation_uid", cfg.operationUID)
			return nil
		}
		bootstrapService, err := agentbootstrap.NewService(
			cfg.stateDir,
			cfg.bootstrapWorkDir,
			commandBootstrapApplier{path: cfg.bootstrapAdapter},
			time.Now,
		)
		if err != nil {
			return err
		}
		applied, err := bootstrapService.Applied(cfg.operationUID)
		if err != nil {
			return fmt.Errorf("check bootstrap success marker: %w", err)
		}
		if applied {
			slog.Info("Provisioning Agent bootstrap apply skipped because success marker already exists", "operation_uid", cfg.operationUID)
			return nil
		}
		bundle, err := apiClient.FetchBootstrap(operationContext, cfg.operationUID, registration.SessionToken)
		if err != nil {
			return err
		}
		if bundle.MachineUID != validatedPlan.Value().Bootstrap.MachineUID ||
			bundle.Format != validatedPlan.Value().Bootstrap.Format {
			return errors.New("bootstrap Bundle does not match Plan target")
		}
		if err := bootstrapService.Apply(operationContext, bundle); err != nil {
			return err
		}
		slog.Info(
			"Provisioning Agent bootstrap applied",
			"operation_uid", cfg.operationUID,
			"format", bundle.Format,
			"payload_digest", bundle.PayloadDigest,
		)
		return nil
	}
	if cfg.reportBoot {
		bootstrapService, err := agentbootstrap.NewService(
			cfg.stateDir,
			cfg.bootstrapWorkDir,
			commandBootstrapApplier{path: cfg.bootstrapAdapter},
			time.Now,
		)
		if err != nil {
			return err
		}
		marker, bootstrapApplied, err := bootstrapService.Marker(cfg.operationUID)
		if err != nil {
			return fmt.Errorf("read bootstrap success marker: %w", err)
		}
		if err := apiClient.ReportBoot(operationContext, registration.SessionToken, agentprotocol.BootReportRequest{
			APIVersion:             agentprotocol.APIVersion,
			OperationUID:           cfg.operationUID,
			PlanDigest:             registration.PlanDigest,
			BootID:                 cfg.bootID,
			ActiveSlot:             cfg.activeSlot,
			ArtifactGeneration:     cfg.artifactGeneration,
			StateMounted:           cfg.stateMounted,
			DataMounted:            cfg.dataMounted,
			BootstrapApplied:       bootstrapApplied,
			BootstrapPayloadDigest: marker.PayloadDigest,
		}); err != nil {
			return err
		}
		slog.Info(
			"Provisioning Agent boot report submitted",
			"operation_uid", cfg.operationUID,
			"active_slot", cfg.activeSlot,
			"artifact_generation", cfg.artifactGeneration,
			"state_mounted", cfg.stateMounted,
			"data_mounted", cfg.dataMounted,
			"bootstrap_applied", bootstrapApplied,
		)
		return nil
	}
	slog.Info(
		"Provisioning Agent preflight completed",
		"operation_uid", cfg.operationUID,
		"target_device", target.Path,
		"disk_count", len(devices),
	)
	return nil
}

func parseConfig(args []string) (config, error) {
	var cfg config
	flags := flag.NewFlagSet("provisioning-agent", flag.ContinueOnError)
	flags.StringVar(&cfg.controllerURL, "controller-url", "", "Agent API HTTPS base URL.")
	flags.StringVar(&cfg.operationUID, "operation-uid", "", "Operation UID assigned by the controller.")
	flags.StringVar(&cfg.hostUID, "host-uid", "", "TartHost UID assigned by the controller.")
	flags.StringVar(&cfg.systemUUID, "system-uuid", "", "Host system UUID. Defaults to Linux DMI product_uuid.")
	flags.StringVar(&cfg.bootMAC, "boot-mac-address", "", "MAC address used to boot the Agent.")
	flags.StringVar(&cfg.tlsCAFile, "tls-ca-file", "", "PEM CA bundle used to verify the Agent API.")
	flags.StringVar(&cfg.planKeyID, "plan-key-id", "", "Trusted Plan signing key ID.")
	flags.StringVar(&cfg.planKeyFile, "plan-key-file", "", "PEM Ed25519 public key used to verify Plans.")
	flags.StringVar(&cfg.artifactKeyID, "artifact-key-id", "", "Trusted OS Artifact signing key ID.")
	flags.StringVar(&cfg.artifactKeyFile, "artifact-key-file", "", "PEM Ed25519 public key used to verify OS Artifacts.")
	flags.StringVar(&cfg.registryConfig, "registry-config", "", "Optional Docker-compatible registry credential file.")
	flags.StringVar(&cfg.bootTrialDriver, "boot-trial-driver", "", "Optional executable that writes boot trial metadata for Update Plans.")
	flags.BoolVar(&cfg.preflight, "preflight-only", false, "Validate registration, signed Plan, and disk selection without writing.")
	flags.BoolVar(
		&cfg.prepareLayout,
		"prepare-layout-only",
		false,
		"Create or validate the amd64 UEFI A/B partition layout, then stop. Provision mode destroys the selected disk.",
	)
	flags.BoolVar(
		&cfg.writePayloads,
		"write-payloads-only",
		false,
		"Verify the signed OS Artifact, prepare the layout, write OS/Verity payloads, and read them back. Provision mode destroys the selected disk.",
	)
	flags.BoolVar(
		&cfg.applyBootstrap,
		"apply-bootstrap-only",
		false,
		"Fetch the single-use Bootstrap Bundle, run the local cloud-config adapter, and delete the applied payload original.",
	)
	flags.BoolVar(
		&cfg.reportBoot,
		"report-boot-only",
		false,
		"Report installed OS boot state, mount state, and Bootstrap success marker digest to the Agent API.",
	)
	flags.StringVar(&cfg.stateDir, "state-dir", "/var/lib/tart", "State directory used by the installed OS bootstrap adapter.")
	flags.StringVar(&cfg.bootstrapWorkDir, "bootstrap-work-dir", "/run/tart/bootstrap", "Temporary directory for the Bootstrap payload original.")
	flags.StringVar(
		&cfg.bootstrapAdapter,
		"bootstrap-adapter",
		"/usr/libexec/tart/apply-cloud-config",
		"Local executable that applies cloud-config. The payload path is passed as the only argument.",
	)
	flags.StringVar(&cfg.bootID, "boot-id", "", "Installed OS boot ID observed by the first-boot reporter.")
	flags.StringVar(&cfg.activeSlot, "active-slot", "", "Installed OS active slot observed by the first-boot reporter.")
	flags.Uint64Var(&cfg.artifactGeneration, "artifact-generation", 0, "Installed OS Artifact generation observed by the first-boot reporter.")
	flags.BoolVar(&cfg.stateMounted, "state-mounted", false, "Set when the first-boot reporter observed the State filesystem mounted.")
	flags.BoolVar(&cfg.dataMounted, "data-mounted", false, "Set when the first-boot reporter observed the Data filesystem mounted.")
	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	if flags.NArg() != 0 {
		return config{}, errors.New("unexpected positional arguments")
	}
	modeCount := 0
	for _, enabled := range []bool{cfg.preflight, cfg.prepareLayout, cfg.writePayloads, cfg.applyBootstrap, cfg.reportBoot} {
		if enabled {
			modeCount++
		}
	}
	if modeCount != 1 {
		return config{}, errors.New("exactly one of --preflight-only, --prepare-layout-only, --write-payloads-only, --apply-bootstrap-only, or --report-boot-only is required")
	}
	required := map[string]string{
		"controller-url":   cfg.controllerURL,
		"operation-uid":    cfg.operationUID,
		"host-uid":         cfg.hostUID,
		"boot-mac-address": cfg.bootMAC,
		"plan-key-id":      cfg.planKeyID,
		"plan-key-file":    cfg.planKeyFile,
	}
	for name, value := range required {
		if value == "" {
			return config{}, fmt.Errorf("--%s is required", name)
		}
	}
	if cfg.writePayloads {
		switch {
		case cfg.artifactKeyID == "":
			return config{}, errors.New("--artifact-key-id is required with --write-payloads-only")
		case cfg.artifactKeyFile == "":
			return config{}, errors.New("--artifact-key-file is required with --write-payloads-only")
		}
	}
	if cfg.applyBootstrap {
		switch {
		case cfg.stateDir == "":
			return config{}, errors.New("--state-dir is required with --apply-bootstrap-only")
		case cfg.bootstrapWorkDir == "":
			return config{}, errors.New("--bootstrap-work-dir is required with --apply-bootstrap-only")
		case cfg.bootstrapAdapter == "":
			return config{}, errors.New("--bootstrap-adapter is required with --apply-bootstrap-only")
		}
	}
	if cfg.reportBoot {
		switch {
		case cfg.stateDir == "":
			return config{}, errors.New("--state-dir is required with --report-boot-only")
		case cfg.bootstrapWorkDir == "":
			return config{}, errors.New("--bootstrap-work-dir is required with --report-boot-only")
		case cfg.bootID == "":
			return config{}, errors.New("--boot-id is required with --report-boot-only")
		case cfg.activeSlot != "A" && cfg.activeSlot != "B":
			return config{}, errors.New("--active-slot must be A or B with --report-boot-only")
		case cfg.artifactGeneration == 0:
			return config{}, errors.New("--artifact-generation is required with --report-boot-only")
		}
	}
	return cfg, nil
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

type commandBootstrapApplier struct {
	path string
}

func (applier commandBootstrapApplier) ApplyCloudConfig(
	ctx context.Context,
	payloadPath string,
	_ agentprotocol.BootstrapBundle,
) error {
	command := exec.CommandContext(ctx, applier.path, payloadPath)
	if err := command.Run(); err != nil {
		return fmt.Errorf("run cloud-config bootstrap adapter: %w", err)
	}
	return nil
}
