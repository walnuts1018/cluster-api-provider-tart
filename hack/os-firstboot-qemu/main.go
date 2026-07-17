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

	operationUID     = "qemu-firstboot-operation"
	hostUID          = "qemu-firstboot-host"
	machineUID       = "qemu-firstboot-machine"
	sessionToken     = "qemu-firstboot-session-token"
	planKeyID        = "e2e-agent-plan"
	rootDiskSerial   = "qemu-firstboot-root"
	targetDiskSerial = "qemu-firstboot-target"
	bootMAC          = "52:54:00:12:34:56"
	activeSlot       = "A"
)

type config struct {
	artifactDir string
	workDir     string
	timeout     time.Duration
	cpu         string
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
	}
	flags := flag.NewFlagSet("os-firstboot-qemu", flag.ContinueOnError)
	flags.StringVar(&cfg.artifactDir, "artifact-dir", cfg.artifactDir, "Path to the mkosi artifact directory containing os.img, vmlinuz, and initrd.")
	flags.StringVar(&cfg.workDir, "work-dir", cfg.workDir, "Working directory used for copied images, injected trust material, and serial logs.")
	flags.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "Overall timeout for the QEMU verification run.")
	flags.StringVar(&cfg.cpu, "cpu", cfg.cpu, "QEMU CPU model.")
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
	return cfg, nil
}

func verify(ctx context.Context, cfg config) error {
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	workDir, err := ensureWorkDir(cfg.workDir)
	if err != nil {
		return err
	}
	cfg.workDir = workDir

	artifactDir, err := filepath.Abs(cfg.artifactDir)
	if err != nil {
		return fmt.Errorf("resolve artifact directory: %w", err)
	}
	serialLogPath := filepath.Join(cfg.workDir, "serial.log")

	paths, err := resolveArtifacts(artifactDir)
	if err != nil {
		return err
	}
	osTestImagePath := filepath.Join(cfg.workDir, "os-test.img")
	if err := copyFile(paths.osImagePath, osTestImagePath); err != nil {
		return fmt.Errorf("copy OS image: %w", err)
	}
	imageInfo, err := os.Stat(osTestImagePath)
	if err != nil {
		return fmt.Errorf("stat copied OS image: %w", err)
	}
	targetDiskPath := filepath.Join(cfg.workDir, "target.raw")
	if err := createTargetDisk(targetDiskPath, imageInfo.Size()); err != nil {
		return err
	}

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
	signedPlan, planDigest, err := buildSignedPlan(planPrivateKey, imageInfo.Size(), deadline)
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

	qemu, err := startQEMU(ctx, cfg, paths, osTestImagePath, targetDiskPath, serialLogPath, server.controllerURL)
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
		if err := qemu.stop(); err != nil {
			slog.Warn("Failed to stop QEMU after successful boot report", "error", err)
		}
		if err := writeEvidence(cfg, paths, osTestImagePath, imageInfo.Size(), bootstrapDigest, report, serialLogPath); err != nil {
			return fmt.Errorf("write QEMU first-boot evidence: %w", err)
		}
		return nil
	case err := <-qemu.exitResult:
		reason := "QEMU exited before the first-boot BootReport arrived"
		if err != nil {
			reason = fmt.Sprintf("%s: %v", reason, err)
		}
		return &verificationError{
			reason:        reason,
			serialLogPath: serialLogPath,
		}
	case <-ctx.Done():
		reason := "timed out waiting for the first-boot BootReport"
		if logHint, err := readLogTail(serialLogPath); err == nil && strings.TrimSpace(logHint) != "" {
			reason = fmt.Sprintf("%s: %s", reason, logHint)
		}
		return &verificationError{
			reason:        reason,
			serialLogPath: serialLogPath,
		}
	}
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
	sourcePath string
	targetPath string
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
		if err := runPrivileged(ctx, "install", "-D", "-m", "0644", file.sourcePath, targetPath); err != nil {
			return fmt.Errorf("inject %s into OS image: %w", file.targetPath, err)
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
	artifacts artifactPaths,
	osImagePath, targetDiskPath, serialLogPath, controllerURL string,
) (*qemuProcess, error) {
	if _, err := exec.LookPath("qemu-system-x86_64"); err != nil {
		return nil, fmt.Errorf("find qemu-system-x86_64: %w", err)
	}
	if err := os.WriteFile(serialLogPath, nil, 0o644); err != nil {
		return nil, fmt.Errorf("create serial log file: %w", err)
	}
	kernelArgs := buildKernelCommandLine(controllerURL)
	args := []string{
		"-accel", qemuAccelerator(),
		"-m", "2048",
		"-smp", "2",
		"-cpu", cfg.cpu,
		"-kernel", artifacts.kernelPath,
		"-initrd", artifacts.initrdPath,
		"-append", kernelArgs,
		"-netdev", "user,id=net0",
		"-device", "virtio-net-pci,netdev=net0,mac=" + bootMAC,
		"-drive", "file=" + osImagePath + ",if=none,id=rootdisk,format=raw",
		"-device", "virtio-blk-pci,drive=rootdisk,serial=" + rootDiskSerial,
		"-drive", "file=" + targetDiskPath + ",if=none,id=targetdisk,format=raw",
		"-device", "virtio-blk-pci,drive=targetdisk,serial=" + targetDiskSerial,
		"-serial", "file:" + serialLogPath,
		"-nographic",
		"-display", "none",
		"-no-reboot",
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
	DiskSizeBytes   int64                           `json:"diskSizeBytes"`
	OSImageDigest   string                          `json:"osImageDigest"`
	KernelPath      string                          `json:"kernelPath"`
	InitrdPath      string                          `json:"initrdPath"`
	BootstrapDigest string                          `json:"bootstrapDigest"`
	BootReport      agentprotocol.BootReportRequest `json:"bootReport"`
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
) error {
	imageDigest, err := fileSHA256(osImagePath)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(evidence{
		CPU:             cfg.cpu,
		DiskSizeBytes:   diskSizeBytes,
		OSImageDigest:   imageDigest,
		KernelPath:      artifacts.kernelPath,
		InitrdPath:      artifacts.initrdPath,
		BootstrapDigest: bootstrapDigest,
		BootReport:      report,
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

func pkixName(commonName string) pkix.Name {
	return pkix.Name{CommonName: commonName}
}

func runPrivileged(ctx context.Context, name string, args ...string) error {
	commandName := name
	commandArgs := args
	if os.Geteuid() != 0 {
		commandName = "sudo"
		commandArgs = append([]string{name}, args...)
	}
	command := exec.CommandContext(ctx, commandName, commandArgs...)
	output, err := command.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, trimmed)
	}
	return nil
}
