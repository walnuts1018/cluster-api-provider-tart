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
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

const (
	defaultArtifactDir = "dist/os-artifact"
	defaultTimeout     = 10 * time.Minute
	defaultCPU         = "qemu64"
	defaultScenario    = scenarioFirstBoot

	operationUID       = "qemu-firstboot-operation"
	hostUID            = "qemu-firstboot-host"
	machineUID         = "qemu-firstboot-machine"
	sessionToken       = "qemu-firstboot-session-token"
	planKeyID          = "e2e-agent-plan"
	rootDiskSerial     = "qemu-firstboot-root"
	targetDiskSerial   = "qemu-firstboot-tgt"
	metadataDiskSerial = "qemu-bootmeta"
	bootMAC            = "52:54:00:12:34:56"
	activeSlot         = "A"

	serialMarkerRootSource          = "TART_QEMU_ROOT_SOURCE="
	serialMarkerRootOptions         = "TART_QEMU_ROOT_OPTIONS="
	serialMarkerRootReadOnly        = "TART_QEMU_ROOT_READ_ONLY="
	serialMarkerBootMetadataWritten = "TART_QEMU_BOOTMETA_WRITTEN="
	serialMarkerBootMetadataSynced  = "TART_QEMU_BOOTMETA_SYNCED="
	serialMarkerBootMetadataRead    = "TART_QEMU_BOOTMETA_READ="
	serialMarkerBootEntrySelected   = "TART_QEMU_BOOTENTRY_SELECTED="
)

const (
	scenarioFirstBoot                = "firstboot"
	scenarioBootTrialMetadataPersist = "boot-trial-metadata-persistence"
	scenarioBootloaderRollback       = "bootloader-rollback"
)

type config struct {
	artifactDir string
	workDir     string
	timeout     time.Duration
	cpu         string
	scenario    string
}

type verificationError struct {
	reason        string
	serialLogPath string
}

func (err *verificationError) Error() string {
	return fmt.Sprintf("%s (serial log: %s)", err.reason, err.serialLogPath)
}

type fakeServer struct {
	server          *http.Server
	listener        net.Listener
	controllerURL   string
	bootReports     chan agentprotocol.BootReportRequest
	signedPlan      agentprotocol.SignedPlan
	bootstrapBundle agentprotocol.BootstrapBundle
	planDigest      string
	expiresAt       time.Time
}

type qemuProcess struct {
	cmd        *exec.Cmd
	exitResult chan error
}

type qemuDrive struct {
	path   string
	id     string
	serial string
}

type firmwareMode string

const (
	firmwareModeDirectKernel firmwareMode = "direct-kernel"
	firmwareModeOVMF         firmwareMode = "ovmf"
)

type qemuBootOptions struct {
	firmware   firmwareMode
	kernelPath string
	initrdPath string
	kernelArgs string
	biosPath   string
}

type rootObservation struct {
	Source          string `json:"source"`
	Options         string `json:"options"`
	MountedReadOnly bool   `json:"mountedReadOnly"`
}

type bootTrialMetadataRecord struct {
	ActiveSlot         string `json:"activeSlot"`
	TargetSlot         string `json:"targetSlot"`
	RollbackSlot       string `json:"rollbackSlot"`
	ArtifactGeneration uint64 `json:"artifactGeneration"`
	RemainingAttempts  int    `json:"remainingAttempts"`
}

type bootTrialMetadataObservation struct {
	Record bootTrialMetadataRecord `json:"record"`
}

type bootEntrySelectionObservation struct {
	SelectedEntry string `json:"selectedEntry"`
}

type bootloaderRollbackAttemptResult struct {
	SerialLogPath  string                        `json:"serialLogPath"`
	SelectedEntry  bootEntrySelectionObservation `json:"selectedEntry"`
	EntryFilenames []string                      `json:"entryFilenames"`
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		slog.Error("OS first-boot QEMU verification failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	return verify(ctx, cfg)
}

func parseConfig(args []string) (config, error) {
	cfg := config{
		artifactDir: defaultArtifactDir,
		timeout:     defaultTimeout,
		cpu:         defaultCPU,
		scenario:    defaultScenario,
	}
	flags := flag.NewFlagSet("os-firstboot-qemu", flag.ContinueOnError)
	flags.StringVar(&cfg.artifactDir, "artifact-dir", cfg.artifactDir, "Path to the mkosi artifact directory containing os.img, vmlinuz, and initrd.")
	flags.StringVar(&cfg.workDir, "work-dir", cfg.workDir, "Working directory used for copied images, injected trust material, and serial logs.")
	flags.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "Overall timeout for the QEMU verification run.")
	flags.StringVar(&cfg.cpu, "cpu", cfg.cpu, "QEMU CPU model.")
	flags.StringVar(&cfg.scenario, "scenario", cfg.scenario, "Verification scenario: firstboot, boot-trial-metadata-persistence, or bootloader-rollback.")
	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	if flags.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	if cfg.timeout <= 0 {
		return config{}, errors.New("--timeout must be greater than zero")
	}
	if strings.TrimSpace(cfg.cpu) == "" {
		return config{}, errors.New("--cpu must not be empty")
	}
	switch cfg.scenario {
	case scenarioFirstBoot, scenarioBootTrialMetadataPersist, scenarioBootloaderRollback:
	default:
		return config{}, fmt.Errorf(
			"--scenario must be one of %q, %q, or %q",
			scenarioFirstBoot,
			scenarioBootTrialMetadataPersist,
			scenarioBootloaderRollback,
		)
	}
	return cfg, nil
}

func verify(ctx context.Context, cfg config) error {
	switch cfg.scenario {
	case scenarioFirstBoot:
		return verifyFirstBoot(ctx, cfg)
	case scenarioBootTrialMetadataPersist:
		return verifyBootTrialMetadataPersistence(ctx, cfg)
	case scenarioBootloaderRollback:
		return verifyBootloaderRollback(ctx, cfg)
	default:
		return fmt.Errorf("unsupported scenario %q", cfg.scenario)
	}
}

func verifyFirstBoot(ctx context.Context, cfg config) error {
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	workspace, err := prepareWorkspace(cfg)
	if err != nil {
		return err
	}
	cfg.workDir = workspace.workDir
	paths, err := resolveArtifacts(workspace.artifactDir)
	if err != nil {
		return err
	}
	osTestImagePath := workspace.osTestImage
	targetDiskPath := workspace.targetDisk
	serialLogPath := workspace.serialLog

	planPublicKeyPath, planPrivateKey, err := generatePlanPublicKey(filepath.Join(cfg.workDir, "agent-plan-public.pem"))
	if err != nil {
		return err
	}
	tlsCert, certPath, err := generateServerCertificate(cfg.workDir)
	if err != nil {
		return err
	}
	activeSlotPath, err := writeTextFile(filepath.Join(cfg.workDir, "active-slot"), activeSlot+"\n")
	if err != nil {
		return err
	}
	artifactGenerationPath, err := writeTextFile(filepath.Join(cfg.workDir, "artifact-generation"), "1\n")
	if err != nil {
		return err
	}
	firstBootDropInPath, err := writeTextFile(filepath.Join(cfg.workDir, "qemu-firstboot.conf"), qemuFirstBootDropIn())
	if err != nil {
		return err
	}

	if err := injectTrustMaterial(ctx, osTestImagePath, cfg.workDir,
		injectedFile{
			sourcePath: planPublicKeyPath,
			targetPath: "/etc/tart/agent-plan-public.pem",
		},
		injectedFile{
			sourcePath: certPath,
			targetPath: "/etc/tart/agent-tls.crt",
		},
		injectedFile{
			sourcePath: activeSlotPath,
			targetPath: "/etc/tart/active-slot",
		},
		injectedFile{
			sourcePath: artifactGenerationPath,
			targetPath: "/etc/tart/artifact-generation",
		},
		injectedFile{
			sourcePath: firstBootDropInPath,
			targetPath: "/etc/systemd/system/tart-first-boot.service.d/10-qemu-smoke.conf",
		},
	); err != nil {
		return err
	}

	bootstrapPayload := []byte("#cloud-config\nwrite_files:\n  - path: /etc/tart/qemu-firstboot\n    permissions: '0644'\n    content: |\n      bootstrapped\n")
	bootstrapDigest := canonicalSHA256(bootstrapPayload)

	deadline := time.Now().Add(cfg.timeout).UTC()
	signedPlan, planDigest, err := buildSignedPlan(planPrivateKey, workspace.diskSizeBytes, deadline)
	if err != nil {
		return err
	}
	bundle := agentprotocol.BootstrapBundle{
		APIVersion:    agentprotocol.APIVersion,
		Format:        agentprotocol.BootstrapFormatCloud,
		Payload:       bootstrapPayload,
		PayloadDigest: bootstrapDigest,
		MachineUID:    machineUID,
		OperationUID:  operationUID,
	}
	server, err := startFakeServer(ctx, tlsCert, signedPlan, bundle, planDigest, deadline)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer shutdownCancel()
		if err := server.server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("Failed to stop fake Agent API", "error", err)
		}
	}()

	qemu, err := startQEMU(
		ctx,
		cfg,
		qemuBootOptions{
			firmware:   firmwareModeDirectKernel,
			kernelPath: paths.kernelPath,
			initrdPath: paths.initrdPath,
			kernelArgs: buildKernelCommandLine(server.controllerURL),
		},
		serialLogPath,
		[]qemuDrive{
			{path: osTestImagePath, id: "rootdisk", serial: rootDiskSerial},
			{path: targetDiskPath, id: "targetdisk", serial: targetDiskSerial},
		},
	)
	if err != nil {
		return err
	}
	defer func() {
		if err := qemu.stop(); err != nil {
			slog.Warn("Failed to stop QEMU", "error", err)
		}
	}()

	select {
	case report := <-server.bootReports:
		return handleBootReportSuccess(ctx, qemu, cfg, workspace, paths, bootstrapDigest, report, serialLogPath)
	case err := <-qemu.exitResult:
		return qemuExitError(err, serialLogPath)
	case <-ctx.Done():
		return qemuTimeoutError(serialLogPath)
	}
}

type workspaceState struct {
	workDir       string
	artifactDir   string
	osTestImage   string
	targetDisk    string
	serialLog     string
	diskSizeBytes int64
}

func prepareWorkspace(cfg config) (workspaceState, error) {
	workDir, err := ensureWorkDir(cfg.workDir)
	if err != nil {
		return workspaceState{}, err
	}
	artifactDir, err := filepath.Abs(cfg.artifactDir)
	if err != nil {
		return workspaceState{}, fmt.Errorf("resolve artifact directory: %w", err)
	}
	paths, err := resolveArtifacts(artifactDir)
	if err != nil {
		return workspaceState{}, err
	}
	osTestImagePath := filepath.Join(workDir, "os-test.img")
	if err := copyFile(paths.osImagePath, osTestImagePath); err != nil {
		return workspaceState{}, fmt.Errorf("copy OS image: %w", err)
	}
	imageInfo, err := os.Stat(osTestImagePath)
	if err != nil {
		return workspaceState{}, fmt.Errorf("stat copied OS image: %w", err)
	}
	targetDiskPath := filepath.Join(workDir, "target.raw")
	if err := createTargetDisk(targetDiskPath, imageInfo.Size()); err != nil {
		return workspaceState{}, err
	}
	return workspaceState{
		workDir:       workDir,
		artifactDir:   artifactDir,
		osTestImage:   osTestImagePath,
		targetDisk:    targetDiskPath,
		serialLog:     filepath.Join(workDir, "serial.log"),
		diskSizeBytes: imageInfo.Size(),
	}, nil
}

func handleBootReportSuccess(
	ctx context.Context,
	qemu *qemuProcess,
	cfg config,
	workspace workspaceState,
	paths artifactPaths,
	bootstrapDigest string,
	report agentprotocol.BootReportRequest,
	serialLogPath string,
) error {
	if err := verifyBootReport(report, bootstrapDigest); err != nil {
		return &verificationError{
			reason:        err.Error(),
			serialLogPath: serialLogPath,
		}
	}
	if ok, err := serialLogContainsOne(serialLogPath,
		"Tart first boot bootstrap and health report",
		"Provisioning Agent boot report submitted",
	); err != nil {
		slog.Warn("Failed to inspect serial log", "error", err, "serial_log", serialLogPath)
	} else if !ok {
		slog.Warn("Boot report was received, but the expected serial log marker was absent", "serial_log", serialLogPath)
	}
	rootObserved, err := waitForRootObservation(ctx, serialLogPath)
	if err != nil {
		return &verificationError{
			reason:        err.Error(),
			serialLogPath: serialLogPath,
		}
	}
	if !rootObserved.MountedReadOnly {
		return &verificationError{
			reason:        fmt.Sprintf("guest root filesystem was not mounted read-only: source=%q options=%q", rootObserved.Source, rootObserved.Options),
			serialLogPath: serialLogPath,
		}
	}
	if err := qemu.stop(); err != nil {
		slog.Warn("Failed to stop QEMU after successful boot report", "error", err)
	}
	if err := writeEvidence(cfg, paths, workspace.osTestImage, workspace.diskSizeBytes, bootstrapDigest, report, serialLogPath, rootObserved); err != nil {
		return fmt.Errorf("write QEMU first-boot evidence: %w", err)
	}
	return nil
}

func qemuExitError(err error, serialLogPath string) error {
	reason := "QEMU exited before the first-boot BootReport arrived"
	if err != nil {
		reason = fmt.Sprintf("%s: %v", reason, err)
	}
	return &verificationError{
		reason:        reason,
		serialLogPath: serialLogPath,
	}
}

func qemuTimeoutError(serialLogPath string) error {
	reason := "timed out waiting for the first-boot BootReport"
	if logHint, err := readLogTail(serialLogPath); err == nil && strings.TrimSpace(logHint) != "" {
		reason = fmt.Sprintf("%s: %s", reason, logHint)
	}
	return &verificationError{
		reason:        reason,
		serialLogPath: serialLogPath,
	}
}

func verifyBootTrialMetadataPersistence(ctx context.Context, cfg config) error {
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	workspace, err := prepareWorkspace(cfg)
	if err != nil {
		return err
	}
	cfg.workDir = workspace.workDir
	paths, err := resolveArtifacts(workspace.artifactDir)
	if err != nil {
		return err
	}
	metadataDiskPath := filepath.Join(cfg.workDir, "boot-metadata.raw")
	if err := createTargetDisk(metadataDiskPath, 1<<20); err != nil {
		return err
	}
	metadataScriptPath, err := writeTextFile(
		filepath.Join(cfg.workDir, "qemu-boot-trial-metadata.sh"),
		qemuBootTrialMetadataScript(),
	)
	if err != nil {
		return err
	}
	metadataServicePath, err := writeTextFile(
		filepath.Join(cfg.workDir, "qemu-boot-trial-metadata.service"),
		qemuBootTrialMetadataUnit(),
	)
	if err != nil {
		return err
	}

	if err := injectTrustMaterial(ctx, workspace.osTestImage, cfg.workDir,
		injectedFile{
			sourcePath: metadataScriptPath,
			targetPath: "/usr/local/lib/tart/qemu-boot-trial-metadata.sh",
			mode:       0o755,
		},
		injectedFile{
			sourcePath: metadataServicePath,
			targetPath: "/etc/systemd/system/qemu-boot-trial-metadata.service",
		},
		injectedFile{
			targetPath:    "/etc/systemd/system/multi-user.target.wants/qemu-boot-trial-metadata.service",
			symlinkTarget: "../qemu-boot-trial-metadata.service",
		},
	); err != nil {
		return err
	}

	firstBootSerialLogPath := filepath.Join(cfg.workDir, "serial-boot1.log")
	firstBootQEMU, err := startQEMU(
		ctx,
		cfg,
		qemuBootOptions{
			firmware:   firmwareModeDirectKernel,
			kernelPath: paths.kernelPath,
			initrdPath: paths.initrdPath,
			kernelArgs: buildKernelCommandLine(""),
		},
		firstBootSerialLogPath,
		[]qemuDrive{
			{path: workspace.osTestImage, id: "rootdisk", serial: rootDiskSerial},
			{path: metadataDiskPath, id: "metadatadisk", serial: metadataDiskSerial},
		},
	)
	if err != nil {
		return err
	}
	firstBootObservation, err := waitForBootTrialMetadataWrite(ctx, firstBootSerialLogPath)
	if stopErr := firstBootQEMU.stop(); stopErr != nil {
		slog.Warn("Failed to stop QEMU after metadata write", "error", stopErr)
	}
	if err != nil {
		return &verificationError{
			reason:        err.Error(),
			serialLogPath: firstBootSerialLogPath,
		}
	}

	secondBootSerialLogPath := filepath.Join(cfg.workDir, "serial-boot2.log")
	secondBootQEMU, err := startQEMU(
		ctx,
		cfg,
		qemuBootOptions{
			firmware:   firmwareModeDirectKernel,
			kernelPath: paths.kernelPath,
			initrdPath: paths.initrdPath,
			kernelArgs: buildKernelCommandLine(""),
		},
		secondBootSerialLogPath,
		[]qemuDrive{
			{path: workspace.osTestImage, id: "rootdisk", serial: rootDiskSerial},
			{path: metadataDiskPath, id: "metadatadisk", serial: metadataDiskSerial},
		},
	)
	if err != nil {
		return err
	}
	secondBootObservation, err := waitForBootTrialMetadataRead(ctx, secondBootSerialLogPath)
	if stopErr := secondBootQEMU.stop(); stopErr != nil {
		slog.Warn("Failed to stop QEMU after metadata read", "error", stopErr)
	}
	if err != nil {
		return &verificationError{
			reason:        err.Error(),
			serialLogPath: secondBootSerialLogPath,
		}
	}
	if firstBootObservation.Record != secondBootObservation.Record {
		return &verificationError{
			reason: fmt.Sprintf(
				"boot metadata did not persist across forced power loss: written=%+v read=%+v",
				firstBootObservation.Record,
				secondBootObservation.Record,
			),
			serialLogPath: secondBootSerialLogPath,
		}
	}
	if err := writeBootTrialMetadataEvidence(
		cfg,
		paths,
		workspace.osTestImage,
		metadataDiskPath,
		firstBootSerialLogPath,
		secondBootSerialLogPath,
		firstBootObservation,
		secondBootObservation,
	); err != nil {
		return fmt.Errorf("write boot metadata persistence evidence: %w", err)
	}
	return nil
}

func verifyBootloaderRollback(ctx context.Context, cfg config) error {
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	workspace, err := prepareWorkspace(cfg)
	if err != nil {
		return err
	}
	cfg.workDir = workspace.workDir
	paths, err := resolveArtifacts(workspace.artifactDir)
	if err != nil {
		return err
	}
	bootEntryScriptPath, err := writeTextFile(
		filepath.Join(cfg.workDir, "qemu-bootloader-rollback.sh"),
		qemuBootloaderRollbackScript(),
	)
	if err != nil {
		return err
	}
	bootEntryServicePath, err := writeTextFile(
		filepath.Join(cfg.workDir, "qemu-bootloader-rollback.service"),
		qemuBootloaderRollbackUnit(),
	)
	if err != nil {
		return err
	}
	if err := injectTrustMaterial(ctx, workspace.osTestImage, cfg.workDir,
		injectedFile{
			targetPath:    "/etc/systemd/system/tart-first-boot.service",
			symlinkTarget: "/dev/null",
		},
		injectedFile{
			targetPath:    "/etc/systemd/system/systemd-bless-boot.service",
			symlinkTarget: "/dev/null",
		},
		injectedFile{
			sourcePath: bootEntryScriptPath,
			targetPath: "/usr/local/lib/tart/qemu-bootloader-rollback.sh",
			mode:       0o755,
		},
		injectedFile{
			sourcePath: bootEntryServicePath,
			targetPath: "/etc/systemd/system/qemu-bootloader-rollback.service",
		},
		injectedFile{
			targetPath:    "/etc/systemd/system/multi-user.target.wants/qemu-bootloader-rollback.service",
			symlinkTarget: "../qemu-bootloader-rollback.service",
		},
	); err != nil {
		return err
	}

	espDiskPath := filepath.Join(cfg.workDir, "esp.raw")
	if err := createBootloaderESPDisk(ctx, cfg.workDir, espDiskPath, paths, "/dev/vdb"); err != nil {
		return err
	}
	biosPath, err := findOVMFFirmware()
	if err != nil {
		return err
	}

	results := make([]bootloaderRollbackAttemptResult, 0, 4)

	for attempt := range 4 {
		serialLogPath := filepath.Join(cfg.workDir, fmt.Sprintf("serial-boot%d.log", attempt+1))
		qemu, err := startQEMU(
			ctx,
			cfg,
			qemuBootOptions{
				firmware: firmwareModeOVMF,
				biosPath: biosPath,
			},
			serialLogPath,
			[]qemuDrive{
				{path: espDiskPath, id: "espdisk", serial: "qemu-esp"},
				{path: workspace.osTestImage, id: "rootdisk", serial: rootDiskSerial},
			},
		)
		if err != nil {
			return err
		}
		selected, err := waitForBootEntrySelection(ctx, serialLogPath)
		if err != nil {
			if stopErr := qemu.stop(); stopErr != nil {
				slog.Warn("Failed to stop QEMU after boot entry selection timeout", "error", stopErr)
			}
			return &verificationError{
				reason:        err.Error(),
				serialLogPath: serialLogPath,
			}
		}
		exitErr := <-qemu.exitResult
		if exitErr != nil {
			return &verificationError{
				reason:        fmt.Sprintf("QEMU exited unexpectedly after boot entry selection: %v", exitErr),
				serialLogPath: serialLogPath,
			}
		}
		entryFilenames, err := bootloaderEntryFilenames(ctx, cfg.workDir, espDiskPath)
		if err != nil {
			return err
		}
		results = append(results, bootloaderRollbackAttemptResult{
			SerialLogPath:  serialLogPath,
			SelectedEntry:  selected,
			EntryFilenames: entryFilenames,
		})
	}

	for attempt := range 3 {
		if results[attempt].SelectedEntry.SelectedEntry != "target" {
			return &verificationError{
				reason:        fmt.Sprintf("boot %d selected %q, want target", attempt+1, results[attempt].SelectedEntry.SelectedEntry),
				serialLogPath: results[attempt].SerialLogPath,
			}
		}
	}
	if results[3].SelectedEntry.SelectedEntry != "rollback" {
		return &verificationError{
			reason:        fmt.Sprintf("boot 4 selected %q, want rollback", results[3].SelectedEntry.SelectedEntry),
			serialLogPath: results[3].SerialLogPath,
		}
	}
	expectedEntries := [][]string{
		{"tart-rollback.conf", "tart-target+2-1.conf"},
		{"tart-rollback.conf", "tart-target+1-2.conf"},
		{"tart-rollback.conf", "tart-target+0-3.conf"},
		{"tart-rollback.conf", "tart-target+0-3.conf"},
	}
	for attempt, want := range expectedEntries {
		if !slices.Equal(results[attempt].EntryFilenames, want) {
			return &verificationError{
				reason: fmt.Sprintf(
					"boot %d entry filenames = %v, want %v",
					attempt+1,
					results[attempt].EntryFilenames,
					want,
				),
				serialLogPath: results[attempt].SerialLogPath,
			}
		}
	}
	if err := writeBootloaderRollbackEvidence(cfg, paths, espDiskPath, workspace.osTestImage, biosPath, results); err != nil {
		return fmt.Errorf("write bootloader rollback evidence: %w", err)
	}
	return nil
}

type artifactPaths struct {
	osImagePath string
	kernelPath  string
	initrdPath  string
}

func resolveArtifacts(artifactDir string) (artifactPaths, error) {
	paths := artifactPaths{
		osImagePath: filepath.Join(artifactDir, "os.img"),
		kernelPath:  filepath.Join(artifactDir, "vmlinuz"),
		initrdPath:  filepath.Join(artifactDir, "initrd"),
	}
	for _, path := range []string{paths.osImagePath, paths.kernelPath, paths.initrdPath} {
		info, err := os.Stat(path)
		if err != nil {
			return artifactPaths{}, fmt.Errorf("stat artifact %s: %w", path, err)
		}
		if info.IsDir() {
			return artifactPaths{}, fmt.Errorf("artifact %s must be a file", path)
		}
	}
	return paths, nil
}

func ensureWorkDir(path string) (string, error) {
	if path == "" {
		dir, err := os.MkdirTemp("", "os-firstboot-qemu-")
		if err != nil {
			return "", fmt.Errorf("create temporary work directory: %w", err)
		}
		return dir, nil
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", fmt.Errorf("create work directory: %w", err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve work directory: %w", err)
	}
	return absPath, nil
}

func copyFile(sourcePath, targetPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer func() {
		_ = source.Close()
	}()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		closeErr := target.Close()
		return errors.Join(err, closeErr)
	}
	if err := target.Close(); err != nil {
		return err
	}
	return nil
}

func createTargetDisk(path string, sizeBytes int64) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create target disk image: %w", err)
	}
	if err := file.Truncate(sizeBytes); err != nil {
		closeErr := file.Close()
		return errors.Join(fmt.Errorf("size target disk image: %w", err), closeErr)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close target disk image: %w", err)
	}
	return nil
}

func createBootloaderESPDisk(
	ctx context.Context,
	workDir, espDiskPath string,
	artifacts artifactPaths,
	rootDevice string,
) error {
	if err := createTargetDisk(espDiskPath, 128<<20); err != nil {
		return err
	}
	layout := strings.Join([]string{
		"label: gpt",
		",96MiB,U,*",
		"",
	}, "\n")
	if err := runPrivilegedInput(ctx, layout, "sfdisk", espDiskPath); err != nil {
		return fmt.Errorf("partition ESP disk: %w", err)
	}
	systemdBootEFIPath, err := findSystemdBootEFI()
	if err != nil {
		return err
	}
	return withLoopPartitions(ctx, espDiskPath, func(partitions []string) error {
		if len(partitions) != 1 {
			return fmt.Errorf("ESP disk partitions = %d, want 1", len(partitions))
		}
		if err := runPrivileged(ctx, "mkfs.fat", "-F", "32", partitions[0]); err != nil {
			return fmt.Errorf("format ESP filesystem: %w", err)
		}
		return withMountedPartition(ctx, workDir, partitions[0], func(mountDir string) error {
			for _, dir := range []string{
				filepath.Join(mountDir, "EFI", "BOOT"),
				filepath.Join(mountDir, "loader", "entries"),
			} {
				if err := runPrivileged(ctx, "mkdir", "-p", dir); err != nil {
					return fmt.Errorf("prepare ESP directory %s: %w", dir, err)
				}
			}
			if err := runPrivileged(ctx, "install", "-D", "-m", "0644", systemdBootEFIPath, filepath.Join(mountDir, "EFI", "BOOT", "BOOTX64.EFI")); err != nil {
				return fmt.Errorf("install systemd-boot EFI binary: %w", err)
			}
			loaderConfPath, err := writeTextFile(filepath.Join(workDir, "loader.conf"), strings.Join([]string{
				"default tart-*",
				"timeout 0",
				"editor no",
				"",
			}, "\n"))
			if err != nil {
				return err
			}
			targetEntryPath, err := writeTextFile(
				filepath.Join(workDir, "tart-target+3.conf"),
				bootloaderEntryConfig("Tart Target", "2", "/vmlinuz", "/initrd", rootDevice, "target"),
			)
			if err != nil {
				return err
			}
			rollbackEntryPath, err := writeTextFile(
				filepath.Join(workDir, "tart-rollback.conf"),
				bootloaderEntryConfig("Tart Rollback", "1", "/vmlinuz", "/initrd", rootDevice, "rollback"),
			)
			if err != nil {
				return err
			}
			for _, file := range []struct {
				source string
				dest   string
			}{
				{source: artifacts.kernelPath, dest: filepath.Join(mountDir, "vmlinuz")},
				{source: artifacts.initrdPath, dest: filepath.Join(mountDir, "initrd")},
				{source: loaderConfPath, dest: filepath.Join(mountDir, "loader", "loader.conf")},
				{source: targetEntryPath, dest: filepath.Join(mountDir, "loader", "entries", "tart-target+3.conf")},
				{source: rollbackEntryPath, dest: filepath.Join(mountDir, "loader", "entries", "tart-rollback.conf")},
			} {
				if err := runPrivileged(ctx, "install", "-D", "-m", "0644", file.source, file.dest); err != nil {
					return fmt.Errorf("install %s into ESP: %w", filepath.Base(file.dest), err)
				}
			}
			return nil
		})
	})
}

func bootloaderEntryConfig(title, version, kernelPath, initrdPath, rootDevice, selectedEntry string) string {
	return strings.Join([]string{
		"title " + title,
		"version " + version,
		"linux " + kernelPath,
		"initrd " + initrdPath,
		"options root=" + rootDevice + " ro console=ttyS0 tart.qemu.boot-entry=" + selectedEntry,
		"",
	}, "\n")
}

func generatePlanPublicKey(targetPath string) (string, ed25519.PrivateKey, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", nil, fmt.Errorf("generate Plan signing key: %w", err)
	}
	publicKeyDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", nil, fmt.Errorf("marshal Plan public key: %w", err)
	}
	block := &pem.Block{Type: "PUBLIC KEY", Bytes: publicKeyDER}
	if err := os.WriteFile(targetPath, pem.EncodeToMemory(block), 0o644); err != nil {
		return "", nil, fmt.Errorf("write Plan public key: %w", err)
	}
	return targetPath, privateKey, nil
}

func generateServerCertificate(workDir string) (tls.Certificate, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("generate TLS private key: %w", err)
	}
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("generate TLS serial number: %w", err)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               pkixName("10.0.2.2"),
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
		IPAddresses: []net.IP{
			net.ParseIP("10.0.2.2"),
			net.ParseIP("127.0.0.1"),
		},
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("create self-signed TLS certificate: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("load generated TLS certificate: %w", err)
	}
	certPath := filepath.Join(workDir, "agent-tls.crt")
	keyPath := filepath.Join(workDir, "agent-tls.key")
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return tls.Certificate{}, "", fmt.Errorf("write TLS certificate: %w", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, "", fmt.Errorf("write TLS private key: %w", err)
	}
	return certificate, certPath, nil
}

type injectedFile struct {
	sourcePath    string
	targetPath    string
	symlinkTarget string
	mode          os.FileMode
}

func injectTrustMaterial(ctx context.Context, imagePath, workDir string, files ...injectedFile) error {
	mountDir := filepath.Join(workDir, "rootfs-mount")
	if err := os.MkdirAll(mountDir, 0o755); err != nil {
		return fmt.Errorf("create mount directory: %w", err)
	}
	mounted := false
	if err := runPrivileged(ctx, "mount", "-o", "loop,rw", imagePath, mountDir); err != nil {
		return fmt.Errorf("mount copied OS image: %w", err)
	}
	mounted = true
	defer func() {
		if mounted {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cleanupCancel()
			if err := runPrivileged(cleanupCtx, "umount", mountDir); err != nil {
				slog.Warn("Failed to unmount copied OS image", "error", err, "mount_dir", mountDir)
			}
		}
	}()

	for _, file := range files {
		targetPath := filepath.Join(mountDir, strings.TrimPrefix(file.targetPath, "/"))
		targetDir := filepath.Dir(targetPath)
		if err := runPrivileged(ctx, "mkdir", "-p", targetDir); err != nil {
			return fmt.Errorf("prepare parent directory for %s: %w", file.targetPath, err)
		}
		switch {
		case file.symlinkTarget != "":
			if err := runPrivileged(ctx, "ln", "-snf", file.symlinkTarget, targetPath); err != nil {
				return fmt.Errorf("inject symlink %s into OS image: %w", file.targetPath, err)
			}
		case file.sourcePath != "":
			mode := file.mode
			if mode == 0 {
				mode = 0o644
			}
			if err := runPrivileged(ctx, "install", "-D", "-m", fmt.Sprintf("%#o", mode.Perm()), file.sourcePath, targetPath); err != nil {
				return fmt.Errorf("inject %s into OS image: %w", file.targetPath, err)
			}
		default:
			return fmt.Errorf("inject %s into OS image: source or symlink target is required", file.targetPath)
		}
	}
	return nil
}

func writeTextFile(path, content string) (string, error) {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

func qemuFirstBootDropIn() string {
	return strings.Join([]string{
		"[Service]",
		"Environment=TART_STATE_DIR=/run/tart/state",
		"Environment=TART_BOOTSTRAP_ADAPTER=/bin/true",
		"Environment=TART_SYSTEM_UUID=00000000-0000-4000-8000-000000000001",
		"",
	}, "\n")
}

func qemuBootTrialMetadataUnit() string {
	return strings.Join([]string{
		"[Unit]",
		"Description=QEMU boot trial metadata persistence smoke",
		"After=local-fs.target",
		"",
		"[Service]",
		"Type=simple",
		"ExecStart=/usr/local/lib/tart/qemu-boot-trial-metadata.sh",
		"StandardOutput=journal+console",
		"StandardError=journal+console",
		"",
		"[Install]",
		"WantedBy=multi-user.target",
		"",
	}, "\n")
}

func qemuBootTrialMetadataScript() string {
	recordJSON := mustJSON(bootTrialMetadataRecord{
		ActiveSlot:         "B",
		TargetSlot:         "B",
		RollbackSlot:       "A",
		ArtifactGeneration: 2,
		RemainingAttempts:  2,
	})
	return strings.Join([]string{
		"#!/bin/sh",
		"set -eu",
		"metadata_device=/dev/disk/by-id/virtio-" + metadataDiskSerial,
		"metadata_record='" + recordJSON + "'",
		"current_record=$(dd if=\"$metadata_device\" bs=256 count=1 status=none 2>/dev/null | tr -d '\\000')",
		"if [ -n \"$current_record\" ]; then",
		"  echo '" + serialMarkerBootMetadataRead + "'\"$current_record\"",
		"  exit 0",
		"fi",
		"printf '%s' \"$metadata_record\" | dd of=\"$metadata_device\" bs=256 conv=fsync,notrunc status=none",
		"echo '" + serialMarkerBootMetadataWritten + recordJSON + "'",
		"sync",
		"echo '" + serialMarkerBootMetadataSynced + "true'",
		"sleep 300",
		"",
	}, "\n")
}

func qemuBootloaderRollbackUnit() string {
	return strings.Join([]string{
		"[Unit]",
		"Description=QEMU bootloader rollback smoke",
		"After=local-fs.target",
		"",
		"[Service]",
		"Type=oneshot",
		"ExecStart=/usr/local/lib/tart/qemu-bootloader-rollback.sh",
		"StandardOutput=journal+console",
		"StandardError=journal+console",
		"",
		"[Install]",
		"WantedBy=multi-user.target",
		"",
	}, "\n")
}

func qemuBootloaderRollbackScript() string {
	return strings.Join([]string{
		"#!/bin/sh",
		"set -eu",
		"selected=unknown",
		"case \"$(cat /proc/cmdline)\" in",
		"  *tart.qemu.boot-entry=target*)",
		"    selected=target",
		"    ;;",
		"  *tart.qemu.boot-entry=rollback*)",
		"    selected=rollback",
		"    ;;",
		"esac",
		"echo '" + serialMarkerBootEntrySelected + "'\"$selected\"",
		"sync",
		"systemctl poweroff --force --force",
		"",
	}, "\n")
}

func buildSignedPlan(
	privateKey ed25519.PrivateKey,
	minSizeBytes int64,
	deadline time.Time,
) (agentprotocol.SignedPlan, string, error) {
	validated, err := agentprotocol.ValidatePlan(agentprotocol.Plan{
		APIVersion:    agentprotocol.APIVersion,
		OperationUID:  operationUID,
		HostUID:       hostUID,
		OperationType: agentprotocol.OperationTypeProvision,
		Deadline:      deadline,
		RootDevice: agentprotocol.RootDevice{
			DeviceName:   "/dev/disk/by-id/virtio-" + targetDiskSerial,
			SerialNumber: targetDiskSerial,
			MinSizeBytes: minSizeBytes,
		},
		Artifact: &agentprotocol.Artifact{
			Ref:            "oci://registry.test.walnuts.dev/os@sha256:" + strings.Repeat("a", 64),
			ManifestDigest: "sha256:" + strings.Repeat("b", 64),
			Generation:     1,
		},
		AllowedTargetRoles: []agentprotocol.DiskRole{agentprotocol.DiskRoleOSA},
		Steps: []agentprotocol.PlanStep{
			{Name: agentprotocol.StepWriteImage},
		},
		Bootstrap: &agentprotocol.BootstrapTarget{
			MachineUID: machineUID,
			Format:     agentprotocol.BootstrapFormatCloud,
		},
	})
	if err != nil {
		return agentprotocol.SignedPlan{}, "", fmt.Errorf("build verification Plan: %w", err)
	}
	signature, err := agentprotocol.Sign(validated, planKeyID, privateKey)
	if err != nil {
		return agentprotocol.SignedPlan{}, "", fmt.Errorf("sign verification Plan: %w", err)
	}
	planDigest, err := validated.Digest()
	if err != nil {
		return agentprotocol.SignedPlan{}, "", fmt.Errorf("digest verification Plan: %w", err)
	}
	return agentprotocol.SignedPlan{
		Plan:      validated.Value(),
		Signature: signature,
	}, planDigest.String(), nil
}

func startFakeServer(
	ctx context.Context,
	certificate tls.Certificate,
	signedPlan agentprotocol.SignedPlan,
	bootstrapBundle agentprotocol.BootstrapBundle,
	planDigest string,
	expiresAt time.Time,
) (*fakeServer, error) {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", "0.0.0.0:0")
	if err != nil {
		return nil, fmt.Errorf("listen for fake Agent API: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	server := &fakeServer{
		listener:        listener,
		controllerURL:   fmt.Sprintf("https://10.0.2.2:%d", port),
		bootReports:     make(chan agentprotocol.BootReportRequest, 1),
		signedPlan:      signedPlan,
		bootstrapBundle: bootstrapBundle,
		planDigest:      planDigest,
		expiresAt:       expiresAt,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/agent/register", server.handleRegister)
	mux.HandleFunc("GET /v1/operations/"+operationUID+"/plan", server.handlePlan)
	mux.HandleFunc("GET /v1/operations/"+operationUID+"/bootstrap", server.handleBootstrap)
	mux.HandleFunc("POST /v1/operations/"+operationUID+"/boot-report", server.handleBootReport)
	server.server = &http.Server{
		Handler: mux,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{certificate},
			MinVersion:   tls.VersionTLS12,
		},
	}

	tlsListener := tls.NewListener(listener, server.server.TLSConfig)
	go func() {
		err := server.server.Serve(tlsListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("Fake Agent API stopped unexpectedly", "error", err)
		}
	}()
	return server, nil
}

func (server *fakeServer) handleRegister(writer http.ResponseWriter, request *http.Request) {
	var body agentprotocol.RegisterRequest
	if err := decodeJSON(request, &body); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "InvalidRegister", err.Error())
		return
	}
	if body.APIVersion != agentprotocol.APIVersion || body.OperationUID != operationUID || body.HostUID != hostUID {
		writeAPIError(writer, http.StatusBadRequest, "InvalidRegister", "register request identity does not match the verification scenario")
		return
	}
	if !inventoryHasDisk(body.Inventory, "/dev/disk/by-id/virtio-"+targetDiskSerial, targetDiskSerial) {
		writeAPIError(writer, http.StatusBadRequest, "MissingRootDisk", "register request did not report the expected virtio disk identity")
		return
	}
	writeJSON(writer, http.StatusOK, agentprotocol.RegisterResponse{
		APIVersion:    agentprotocol.APIVersion,
		SessionToken:  sessionToken,
		ExpiresAt:     server.expiresAt,
		PlanDigest:    server.planDigest,
		AgentSequence: 0,
	})
}

func (server *fakeServer) handlePlan(writer http.ResponseWriter, request *http.Request) {
	if !hasSessionToken(request) {
		writeAPIError(writer, http.StatusUnauthorized, "Unauthorized", "missing or invalid session token")
		return
	}
	writeJSON(writer, http.StatusOK, server.signedPlan)
}

func (server *fakeServer) handleBootstrap(writer http.ResponseWriter, request *http.Request) {
	if !hasSessionToken(request) {
		writeAPIError(writer, http.StatusUnauthorized, "Unauthorized", "missing or invalid session token")
		return
	}
	writeJSON(writer, http.StatusOK, server.bootstrapBundle)
}

func (server *fakeServer) handleBootReport(writer http.ResponseWriter, request *http.Request) {
	if !hasSessionToken(request) {
		writeAPIError(writer, http.StatusUnauthorized, "Unauthorized", "missing or invalid session token")
		return
	}
	var body agentprotocol.BootReportRequest
	if err := decodeJSON(request, &body); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "InvalidBootReport", err.Error())
		return
	}
	if err := agentprotocol.ValidateBootReport(body); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "InvalidBootReport", err.Error())
		return
	}
	select {
	case server.bootReports <- body:
	default:
	}
	writer.WriteHeader(http.StatusNoContent)
}

func inventoryHasDisk(inventory agentprotocol.Inventory, byIDPath, serial string) bool {
	for _, disk := range inventory.Disks {
		if disk.SerialNumber != serial {
			continue
		}
		if slices.Contains(disk.ByIDPaths, byIDPath) {
			return true
		}
	}
	return false
}

func hasSessionToken(request *http.Request) bool {
	return request.Header.Get("Authorization") == "Bearer "+sessionToken
}

func startQEMU(
	ctx context.Context,
	cfg config,
	options qemuBootOptions,
	serialLogPath string,
	extraDrives []qemuDrive,
) (*qemuProcess, error) {
	if _, err := exec.LookPath("qemu-system-x86_64"); err != nil {
		return nil, fmt.Errorf("find qemu-system-x86_64: %w", err)
	}
	if err := os.WriteFile(serialLogPath, nil, 0o644); err != nil {
		return nil, fmt.Errorf("create serial log file: %w", err)
	}
	args := []string{
		"-accel", qemuAccelerator(),
		"-m", "2048",
		"-smp", "2",
		"-cpu", cfg.cpu,
		"-serial", "file:" + serialLogPath,
		"-nographic",
		"-display", "none",
		"-no-reboot",
	}
	switch options.firmware {
	case firmwareModeDirectKernel:
		args = append(args,
			"-kernel", options.kernelPath,
			"-initrd", options.initrdPath,
			"-append", options.kernelArgs,
			"-netdev", "user,id=net0",
			"-device", "virtio-net-pci,netdev=net0,mac="+bootMAC,
		)
	case firmwareModeOVMF:
		args = append(args, "-bios", options.biosPath)
	default:
		return nil, fmt.Errorf("unsupported QEMU firmware mode %q", options.firmware)
	}
	for _, drive := range extraDrives {
		args = append(args,
			"-drive", "file="+drive.path+",if=none,id="+drive.id+",format=raw",
			"-device", "virtio-blk-pci,drive="+drive.id+",serial="+drive.serial,
		)
	}
	cmd := exec.CommandContext(ctx, "qemu-system-x86_64", args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start QEMU: %w", err)
	}
	process := &qemuProcess{
		cmd:        cmd,
		exitResult: make(chan error, 1),
	}
	go func() {
		process.exitResult <- cmd.Wait()
	}()
	return process, nil
}

func qemuAccelerator() string {
	if _, err := os.Stat("/dev/kvm"); err == nil {
		return "kvm"
	}
	return "tcg"
}

type evidence struct {
	CPU             string                          `json:"cpu"`
	Scenario        string                          `json:"scenario"`
	DiskSizeBytes   int64                           `json:"diskSizeBytes"`
	OSImageDigest   string                          `json:"osImageDigest"`
	KernelPath      string                          `json:"kernelPath"`
	InitrdPath      string                          `json:"initrdPath"`
	BootstrapDigest string                          `json:"bootstrapDigest"`
	BootReport      agentprotocol.BootReportRequest `json:"bootReport"`
	Root            rootObservation                 `json:"root"`
	SerialLogPath   string                          `json:"serialLogPath"`
}

func writeEvidence(
	cfg config,
	artifacts artifactPaths,
	osImagePath string,
	diskSizeBytes int64,
	bootstrapDigest string,
	report agentprotocol.BootReportRequest,
	serialLogPath string,
	root rootObservation,
) error {
	imageDigest, err := fileSHA256(osImagePath)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(evidence{
		CPU:             cfg.cpu,
		Scenario:        cfg.scenario,
		DiskSizeBytes:   diskSizeBytes,
		OSImageDigest:   imageDigest,
		KernelPath:      artifacts.kernelPath,
		InitrdPath:      artifacts.initrdPath,
		BootstrapDigest: bootstrapDigest,
		BootReport:      report,
		Root:            root,
		SerialLogPath:   serialLogPath,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode evidence: %w", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.workDir, "evidence.json"), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write evidence.json: %w", err)
	}
	return nil
}

type bootTrialMetadataEvidence struct {
	CPU                     string                       `json:"cpu"`
	Scenario                string                       `json:"scenario"`
	OSImageDigest           string                       `json:"osImageDigest"`
	MetadataDiskDigest      string                       `json:"metadataDiskDigest"`
	KernelPath              string                       `json:"kernelPath"`
	InitrdPath              string                       `json:"initrdPath"`
	Written                 bootTrialMetadataObservation `json:"written"`
	ObservedAfterPowerLoss  bootTrialMetadataObservation `json:"observedAfterPowerLoss"`
	FirstBootSerialLogPath  string                       `json:"firstBootSerialLogPath"`
	SecondBootSerialLogPath string                       `json:"secondBootSerialLogPath"`
}

type bootloaderRollbackAttemptEvidence struct {
	Attempt        int      `json:"attempt"`
	SelectedEntry  string   `json:"selectedEntry"`
	EntryFilenames []string `json:"entryFilenames"`
	SerialLogPath  string   `json:"serialLogPath"`
}

type bootloaderRollbackEvidence struct {
	CPU           string                              `json:"cpu"`
	Scenario      string                              `json:"scenario"`
	BIOSPath      string                              `json:"biosPath"`
	OSImageDigest string                              `json:"osImageDigest"`
	ESPDiskDigest string                              `json:"espDiskDigest"`
	KernelPath    string                              `json:"kernelPath"`
	InitrdPath    string                              `json:"initrdPath"`
	Attempts      []bootloaderRollbackAttemptEvidence `json:"attempts"`
}

func writeBootTrialMetadataEvidence(
	cfg config,
	artifacts artifactPaths,
	osImagePath string,
	metadataDiskPath string,
	firstBootSerialLogPath string,
	secondBootSerialLogPath string,
	written bootTrialMetadataObservation,
	observedAfterPowerLoss bootTrialMetadataObservation,
) error {
	imageDigest, err := fileSHA256(osImagePath)
	if err != nil {
		return err
	}
	metadataDiskDigest, err := fileSHA256(metadataDiskPath)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(bootTrialMetadataEvidence{
		CPU:                     cfg.cpu,
		Scenario:                cfg.scenario,
		OSImageDigest:           imageDigest,
		MetadataDiskDigest:      metadataDiskDigest,
		KernelPath:              artifacts.kernelPath,
		InitrdPath:              artifacts.initrdPath,
		Written:                 written,
		ObservedAfterPowerLoss:  observedAfterPowerLoss,
		FirstBootSerialLogPath:  firstBootSerialLogPath,
		SecondBootSerialLogPath: secondBootSerialLogPath,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode boot metadata evidence: %w", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.workDir, "evidence.json"), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write evidence.json: %w", err)
	}
	return nil
}

func writeBootloaderRollbackEvidence(
	cfg config,
	artifacts artifactPaths,
	espDiskPath string,
	osImagePath string,
	biosPath string,
	results []bootloaderRollbackAttemptResult,
) error {
	osImageDigest, err := fileSHA256(osImagePath)
	if err != nil {
		return err
	}
	espDiskDigest, err := fileSHA256(espDiskPath)
	if err != nil {
		return err
	}
	attempts := make([]bootloaderRollbackAttemptEvidence, 0, len(results))
	for index, result := range results {
		attempts = append(attempts, bootloaderRollbackAttemptEvidence{
			Attempt:        index + 1,
			SelectedEntry:  result.SelectedEntry.SelectedEntry,
			EntryFilenames: slices.Clone(result.EntryFilenames),
			SerialLogPath:  result.SerialLogPath,
		})
	}
	data, err := json.MarshalIndent(bootloaderRollbackEvidence{
		CPU:           cfg.cpu,
		Scenario:      cfg.scenario,
		BIOSPath:      biosPath,
		OSImageDigest: osImageDigest,
		ESPDiskDigest: espDiskDigest,
		KernelPath:    artifacts.kernelPath,
		InitrdPath:    artifacts.initrdPath,
		Attempts:      attempts,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode bootloader rollback evidence: %w", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.workDir, "evidence.json"), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write evidence.json: %w", err)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open file for digest: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("hash file: %w", err)
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}

func buildKernelCommandLine(controllerURL string) string {
	return strings.Join([]string{
		"root=/dev/vda",
		"ro",
		"console=ttyS0",
		"ip=dhcp",
		"tart.agent.controller-url=" + controllerURL,
		"tart.agent.operation-uid=" + operationUID,
		"tart.agent.host-uid=" + hostUID,
		"tart.agent.boot-mac=" + bootMAC,
	}, " ")
}

func (process *qemuProcess) stop() error {
	if process == nil || process.cmd == nil || process.cmd.Process == nil {
		return nil
	}
	pid := process.cmd.Process.Pid
	err := syscall.Kill(-pid, syscall.SIGKILL)
	switch {
	case err == nil:
	case errors.Is(err, syscall.ESRCH):
		return nil
	default:
		return fmt.Errorf("kill QEMU process group: %w", err)
	}
	select {
	case <-process.exitResult:
	case <-time.After(5 * time.Second):
	}
	return nil
}

func verifyBootReport(report agentprotocol.BootReportRequest, bootstrapDigest string) error {
	switch {
	case report.APIVersion != agentprotocol.APIVersion:
		return errors.New("boot report apiVersion did not match the Agent protocol")
	case report.OperationUID != operationUID:
		return errors.New("boot report operationUID did not match the verification scenario")
	case report.PlanDigest == "":
		return errors.New("boot report planDigest was empty")
	case !report.BootstrapApplied:
		return errors.New("boot report did not confirm bootstrapApplied=true")
	case report.ActiveSlot != activeSlot:
		return fmt.Errorf("boot report activeSlot = %q, want %q", report.ActiveSlot, activeSlot)
	case report.ArtifactGeneration != 1:
		return fmt.Errorf("boot report artifactGeneration = %d, want 1", report.ArtifactGeneration)
	case report.BootstrapPayloadDigest != bootstrapDigest:
		return fmt.Errorf("boot report bootstrapPayloadDigest = %q, want %q", report.BootstrapPayloadDigest, bootstrapDigest)
	default:
		return nil
	}
}

func waitForRootObservation(ctx context.Context, serialLogPath string) (rootObservation, error) {
	deadline := time.Now().Add(10 * time.Second)
	if value, ok, hasReadOnly := rootObservationFromLog(serialLogPath); ok && hasReadOnly {
		return value, nil
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return rootObservation{}, errors.New("timed out waiting for read-only root evidence in the serial log")
		case <-ticker.C:
			value, ok, hasReadOnly := rootObservationFromLog(serialLogPath)
			if ok && hasReadOnly {
				return value, nil
			}
			if time.Now().After(deadline) {
				return rootObservation{}, errors.New("timed out waiting for read-only root evidence in the serial log")
			}
		}
	}
}

func waitForBootTrialMetadataWrite(ctx context.Context, serialLogPath string) (bootTrialMetadataObservation, error) {
	deadline := time.Now().Add(10 * time.Second)
	if value, synced := bootTrialMetadataWriteFromLog(serialLogPath); synced {
		return value, nil
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return bootTrialMetadataObservation{}, errors.New("timed out waiting for boot metadata write evidence in the serial log")
		case <-ticker.C:
			value, synced := bootTrialMetadataWriteFromLog(serialLogPath)
			if synced {
				return value, nil
			}
			if time.Now().After(deadline) {
				return bootTrialMetadataObservation{}, errors.New("timed out waiting for boot metadata sync evidence in the serial log")
			}
		}
	}
}

func waitForBootTrialMetadataRead(ctx context.Context, serialLogPath string) (bootTrialMetadataObservation, error) {
	deadline := time.Now().Add(10 * time.Second)
	if value, ok := bootTrialMetadataReadFromLog(serialLogPath); ok {
		return value, nil
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return bootTrialMetadataObservation{}, errors.New("timed out waiting for boot metadata read evidence in the serial log")
		case <-ticker.C:
			value, ok := bootTrialMetadataReadFromLog(serialLogPath)
			if ok {
				return value, nil
			}
			if time.Now().After(deadline) {
				return bootTrialMetadataObservation{}, errors.New("timed out waiting for boot metadata read evidence in the serial log")
			}
		}
	}
}

func rootObservationFromLog(path string) (rootObservation, bool, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return rootObservation{}, false, false
	}
	lines := strings.Split(string(data), "\n")
	observed := rootObservation{}
	hasSource := false
	hasReadOnly := false

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, serialMarkerRootSource):
			observed.Source = strings.TrimSpace(strings.TrimPrefix(line, serialMarkerRootSource))
			hasSource = true
		case strings.HasPrefix(line, serialMarkerRootOptions):
			observed.Options = strings.TrimSpace(strings.TrimPrefix(line, serialMarkerRootOptions))
		case strings.HasPrefix(line, serialMarkerRootReadOnly):
			observed.MountedReadOnly = strings.TrimSpace(strings.TrimPrefix(line, serialMarkerRootReadOnly)) == "true"
			hasReadOnly = true
		}
	}
	return observed, hasSource, hasReadOnly
}

func bootTrialMetadataWriteFromLog(path string) (bootTrialMetadataObservation, bool) {
	return bootTrialMetadataFromLog(path, serialMarkerBootMetadataWritten, true)
}

func bootTrialMetadataReadFromLog(path string) (bootTrialMetadataObservation, bool) {
	return bootTrialMetadataFromLog(path, serialMarkerBootMetadataRead, false)
}

func bootTrialMetadataFromLog(path, recordMarker string, requireSync bool) (bootTrialMetadataObservation, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return bootTrialMetadataObservation{}, false
	}
	lines := strings.Split(string(data), "\n")
	observation := bootTrialMetadataObservation{}
	hasRecord := false
	hasSync := !requireSync

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, recordMarker):
			record, err := parseBootTrialMetadataRecord(strings.TrimSpace(strings.TrimPrefix(line, recordMarker)))
			if err != nil {
				return bootTrialMetadataObservation{}, false
			}
			observation.Record = record
			hasRecord = true
		case requireSync && strings.HasPrefix(line, serialMarkerBootMetadataSynced):
			hasSync = strings.TrimSpace(strings.TrimPrefix(line, serialMarkerBootMetadataSynced)) == "true"
		}
	}
	return observation, hasRecord && hasSync
}

func parseBootTrialMetadataRecord(raw string) (bootTrialMetadataRecord, error) {
	var record bootTrialMetadataRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return bootTrialMetadataRecord{}, fmt.Errorf("decode boot metadata record: %w", err)
	}
	return record, nil
}

func waitForBootEntrySelection(ctx context.Context, serialLogPath string) (bootEntrySelectionObservation, error) {
	deadline := time.Now().Add(30 * time.Second)
	if value, ok := bootEntrySelectionFromLog(serialLogPath); ok {
		return value, nil
	}
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return bootEntrySelectionObservation{}, errors.New("timed out waiting for boot entry selection evidence in the serial log")
		case <-ticker.C:
			value, ok := bootEntrySelectionFromLog(serialLogPath)
			if ok {
				return value, nil
			}
			if time.Now().After(deadline) {
				return bootEntrySelectionObservation{}, errors.New("timed out waiting for boot entry selection evidence in the serial log")
			}
		}
	}
}

func bootEntrySelectionFromLog(path string) (bootEntrySelectionObservation, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return bootEntrySelectionObservation{}, false
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if after, ok := strings.CutPrefix(line, serialMarkerBootEntrySelected); ok {
			selectedEntry := strings.TrimSpace(after)
			if selectedEntry == "" {
				return bootEntrySelectionObservation{}, false
			}
			return bootEntrySelectionObservation{SelectedEntry: selectedEntry}, true
		}
	}
	return bootEntrySelectionObservation{}, false
}

func bootloaderEntryFilenames(ctx context.Context, workDir, espDiskPath string) ([]string, error) {
	var filenames []string
	if err := withLoopPartitions(ctx, espDiskPath, func(partitions []string) error {
		if len(partitions) != 1 {
			return fmt.Errorf("ESP disk partitions = %d, want 1", len(partitions))
		}
		return withMountedPartition(ctx, workDir, partitions[0], func(mountDir string) error {
			entriesDir := filepath.Join(mountDir, "loader", "entries")
			entries, err := os.ReadDir(entriesDir)
			if err != nil {
				return fmt.Errorf("read loader entries: %w", err)
			}
			filenames = filenames[:0]
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				filenames = append(filenames, entry.Name())
			}
			slices.Sort(filenames)
			return nil
		})
	}); err != nil {
		return nil, err
	}
	return filenames, nil
}

func serialLogContainsOne(path string, needles ...string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	text := string(data)
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true, nil
		}
	}
	return false, nil
}

func readLogTail(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return "", nil
	}
	start := max(len(lines)-80, 0)
	return strings.Join(lines[start:], " | "), nil
}

func decodeJSON(request *http.Request, target any) error {
	defer func() {
		_ = request.Body.Close()
	}()
	decoder := json.NewDecoder(io.LimitReader(request.Body, agentprotocol.MaxBootstrapBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode JSON request: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("JSON request must contain a single document")
	}
	return nil
}

func writeJSON(writer http.ResponseWriter, statusCode int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(body)
}

func writeAPIError(writer http.ResponseWriter, statusCode int, code, message string) {
	writeJSON(writer, statusCode, agentprotocol.ErrorResponse{
		APIVersion: agentprotocol.APIVersion,
		Code:       code,
		Message:    message,
	})
}

func canonicalSHA256(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func pkixName(commonName string) pkix.Name {
	return pkix.Name{CommonName: commonName}
}

func findOVMFFirmware() (string, error) {
	for _, path := range []string{
		"/usr/share/ovmf/OVMF.fd",
		"/usr/share/OVMF/OVMF.fd",
		"/usr/share/qemu/ovmf-x86_64.bin",
	} {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", errors.New("find OVMF firmware: supported OVMF.fd path was not found")
}

func findSystemdBootEFI() (string, error) {
	for _, path := range []string{
		"/usr/lib/systemd/boot/efi/systemd-bootx64.efi",
		"/usr/lib/systemd/boot/efi/systemd-bootx64.efi.signed",
		"/lib/systemd/boot/efi/systemd-bootx64.efi",
	} {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", errors.New("find systemd-boot EFI binary: supported path was not found")
}

func withLoopPartitions(ctx context.Context, imagePath string, fn func([]string) error) error {
	loopDevice, err := runPrivilegedOutput(ctx, "losetup", "--find", "--show", "--partscan", imagePath)
	if err != nil {
		return fmt.Errorf("attach loop device: %w", err)
	}
	loopDevice = strings.TrimSpace(loopDevice)
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cleanupCancel()
		if err := runPrivileged(cleanupCtx, "losetup", "-d", loopDevice); err != nil {
			slog.Warn("Failed to detach loop device", "error", err, "loop_device", loopDevice)
		}
	}()
	partitions, err := filepath.Glob(loopDevice + "p*")
	if err != nil {
		return fmt.Errorf("resolve loop partitions: %w", err)
	}
	if len(partitions) == 0 {
		return fmt.Errorf("loop device %s has no partitions", loopDevice)
	}
	slices.Sort(partitions)
	return fn(partitions)
}

func withMountedPartition(ctx context.Context, workDir, partitionPath string, fn func(string) error) error {
	mountDir, err := os.MkdirTemp(workDir, "partition-mount-")
	if err != nil {
		return fmt.Errorf("create partition mount directory: %w", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(mountDir); removeErr != nil {
			slog.Warn("Failed to remove partition mount directory", "error", removeErr, "mount_dir", mountDir)
		}
	}()
	if err := runPrivileged(ctx, "mount", partitionPath, mountDir); err != nil {
		return fmt.Errorf("mount partition %s: %w", partitionPath, err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cleanupCancel()
		if err := runPrivileged(cleanupCtx, "umount", mountDir); err != nil {
			slog.Warn("Failed to unmount partition", "error", err, "mount_dir", mountDir)
		}
	}()
	return fn(mountDir)
}

func runPrivileged(ctx context.Context, name string, args ...string) error {
	_, err := runPrivilegedCommand(ctx, "", name, args...)
	return err
}

func runPrivilegedInput(ctx context.Context, input string, name string, args ...string) error {
	_, err := runPrivilegedCommand(ctx, input, name, args...)
	return err
}

func runPrivilegedOutput(ctx context.Context, name string, args ...string) (string, error) {
	return runPrivilegedCommand(ctx, "", name, args...)
}

func runPrivilegedCommand(ctx context.Context, input string, name string, args ...string) (string, error) {
	commandName := name
	commandArgs := args
	if os.Geteuid() != 0 {
		commandName = "sudo"
		commandArgs = append([]string{name}, args...)
	}
	command := exec.CommandContext(ctx, commandName, commandArgs...)
	if input != "" {
		command.Stdin = strings.NewReader(input)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", err, trimmed)
	}
	return strings.TrimSpace(string(output)), nil
}
