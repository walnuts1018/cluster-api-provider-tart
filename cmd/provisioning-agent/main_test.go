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
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	agentboot "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/agentboot"
	agentprotocol "github.com/walnuts1018/cluster-api-provider-tart/dto/agent"
	"github.com/walnuts1018/cluster-api-provider-tart/infrastructure/provisioning_agent/disk"
)

func TestParseConfigRequiresExplicitPreflightInputs(t *testing.T) {
	cfg, err := parseConfig([]string{
		"--controller-url=https://controller.test.walnuts.dev",
		"--operation-uid=operation-uid",
		"--host-uid=host-uid",
		"--boot-mac-address=00:11:22:33:44:55",
		"--plan-key-id=test-key",
		"--plan-key-file=/trust/plan.pem",
		"--preflight-only",
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if !cfg.preflight || cfg.planKeyID != "test-key" {
		t.Fatalf("parseConfig() = %#v", cfg)
	}
	if _, err := parseConfig([]string{"--controller-url=https://controller.test.walnuts.dev"}); err == nil {
		t.Fatal("parseConfig() accepted missing identity and trust inputs")
	}
}

func TestParseConfigRequiresExactlyOneMode(t *testing.T) {
	base := []string{
		"--controller-url=https://controller.test.walnuts.dev",
		"--operation-uid=operation-uid",
		"--host-uid=host-uid",
		"--boot-mac-address=00:11:22:33:44:55",
		"--plan-key-id=test-key",
		"--plan-key-file=/trust/plan.pem",
	}
	if _, err := parseConfig(base); err == nil {
		t.Fatal("parseConfig() accepted no diagnostic mode")
	}
	both := append(base, "--preflight-only", "--prepare-layout-only")
	if _, err := parseConfig(both); err == nil {
		t.Fatal("parseConfig() accepted both diagnostic modes")
	}
	prepare := append(base, "--prepare-layout-only")
	cfg, err := parseConfig(prepare)
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if !cfg.prepareLayout {
		t.Fatalf("parseConfig() = %#v", cfg)
	}
	applyBootstrap := append(base, "--apply-bootstrap-only")
	cfg, err = parseConfig(applyBootstrap)
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if !cfg.applyBootstrap || cfg.stateDir == "" || cfg.bootstrapWorkDir == "" || cfg.bootstrapAdapter == "" {
		t.Fatalf("parseConfig() = %#v", cfg)
	}
	reportBoot := append(
		base,
		"--report-boot-only",
		"--boot-id=boot-id",
		"--active-slot=A",
		"--artifact-generation=1",
		"--state-mounted",
		"--data-mounted",
	)
	cfg, err = parseConfig(reportBoot)
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if !cfg.reportBoot || cfg.bootID != "boot-id" || cfg.activeSlot != "A" || cfg.artifactGeneration != 1 {
		t.Fatalf("parseConfig() = %#v", cfg)
	}
	write := append(
		base,
		"--write-payloads-only",
		"--artifact-key-id=artifact-key",
		"--artifact-key-file=/trust/artifact.pem",
	)
	cfg, err = parseConfig(write)
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if !cfg.writePayloads || cfg.artifactKeyID != "artifact-key" {
		t.Fatalf("parseConfig() = %#v", cfg)
	}
	missingArtifactTrust := append(base, "--write-payloads-only")
	if _, err := parseConfig(missingArtifactTrust); err == nil {
		t.Fatal("parseConfig() accepted payload mode without Artifact trust inputs")
	}
	provision := append(
		base,
		"--provision",
		"--artifact-key-id=artifact-key",
		"--artifact-key-file=/trust/artifact.pem",
		"--efi-commit-driver=/usr/libexec/tart/efi-commit",
	)
	cfg, err = parseConfig(provision)
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if !cfg.provision || cfg.efiCommitDriver == "" {
		t.Fatalf("parseConfig() = %#v", cfg)
	}
	if _, err := parseConfig(append(base, "--provision", "--artifact-key-id=artifact-key", "--artifact-key-file=/trust/artifact.pem")); err == nil {
		t.Fatal("parseConfig() accepted Provision mode without EFI commit driver")
	}
}

func TestParseConfigUsesKernelCommandLineAgentInputs(t *testing.T) {
	restore := swapKernelCommandLineReader(func() ([]byte, error) {
		return []byte(stringsJoinFields(
			"quiet",
			"tart.agent.controller-url=https://controller.test.walnuts.dev",
			"tart.agent.operation-uid=operation-from-kcmdline",
			"tart.agent.host-uid=host-from-kcmdline",
			"tart.agent.boot-mac=aa:bb:cc:dd:ee:ff",
		)), nil
	})
	defer restore()

	cfg, err := parseConfig([]string{
		"--plan-key-id=test-key",
		"--plan-key-file=/trust/plan.pem",
		"--preflight-only",
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.controllerURL != "https://controller.test.walnuts.dev" ||
		cfg.operationUID != "operation-from-kcmdline" ||
		cfg.hostUID != "host-from-kcmdline" ||
		cfg.bootMAC != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("parseConfig() = %#v", cfg)
	}
}

func TestParseConfigPrefersExplicitFlagsOverKernelCommandLine(t *testing.T) {
	restore := swapKernelCommandLineReader(func() ([]byte, error) {
		return []byte(stringsJoinFields(
			"tart.agent.controller-url=https://kernel.test.walnuts.dev",
			"tart.agent.operation-uid=operation-from-kcmdline",
			"tart.agent.host-uid=host-from-kcmdline",
			"tart.agent.boot-mac=aa:bb:cc:dd:ee:ff",
		)), nil
	})
	defer restore()

	cfg, err := parseConfig([]string{
		"--controller-url=https://flag.test.walnuts.dev",
		"--operation-uid=operation-from-flag",
		"--host-uid=host-from-flag",
		"--boot-mac-address=00:11:22:33:44:55",
		"--plan-key-id=test-key",
		"--plan-key-file=/trust/plan.pem",
		"--preflight-only",
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.controllerURL != "https://flag.test.walnuts.dev" ||
		cfg.operationUID != "operation-from-flag" ||
		cfg.hostUID != "host-from-flag" ||
		cfg.bootMAC != "00:11:22:33:44:55" {
		t.Fatalf("parseConfig() = %#v", cfg)
	}
}

func TestParseConfigIgnoresMissingKernelCommandLineOnNonLinuxHosts(t *testing.T) {
	restore := swapKernelCommandLineReader(func() ([]byte, error) {
		return nil, os.ErrNotExist
	})
	defer restore()

	if _, err := parseConfig([]string{
		"--controller-url=https://controller.test.walnuts.dev",
		"--operation-uid=operation-uid",
		"--host-uid=host-uid",
		"--boot-mac-address=00:11:22:33:44:55",
		"--plan-key-id=test-key",
		"--plan-key-file=/trust/plan.pem",
		"--preflight-only",
	}); err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
}

func TestBuildRegisterRequestは明示flagの設定をそのまま反映する(t *testing.T) {
	t.Parallel()

	cfg, err := parseConfig([]string{
		"--controller-url=https://controller.test.walnuts.dev",
		"--operation-uid=operation-uid",
		"--host-uid=host-uid",
		"--system-uuid=system-from-flag",
		"--boot-mac-address=00:11:22:33:44:55",
		"--plan-key-id=test-key",
		"--plan-key-file=/trust/plan.pem",
		"--preflight-only",
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}

	got := buildRegisterRequest(cfg, cfg.systemUUID, "agent-instance", sampleRegisterDevices())
	want := agentprotocol.RegisterRequest{
		APIVersion:      agentprotocol.APIVersion,
		OperationUID:    "operation-uid",
		HostUID:         "host-uid",
		AgentInstanceID: "agent-instance",
		Inventory: agentprotocol.Inventory{
			SystemUUID:     "system-from-flag",
			BootMACAddress: "00:11:22:33:44:55",
			Disks: []agentprotocol.DiskInventory{
				{
					DevicePath:   "/dev/nvme0n1",
					ByIDPaths:    []string{"/dev/disk/by-id/nvme-test"},
					SerialNumber: "serial-1",
					WWN:          "wwn-1",
					SizeBytes:    1024,
					HoldsAgentOS: true,
				},
				{
					DevicePath:   "/dev/sda",
					ByIDPaths:    []string{"/dev/disk/by-id/ata-test", "/dev/disk/by-id/wwn-test"},
					SerialNumber: "serial-2",
					WWN:          "wwn-2",
					SizeBytes:    2048,
					HoldsAgentOS: false,
				},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildRegisterRequest() = %#v, want %#v", got, want)
	}
}

func TestBuildRegisterRequestはkernelCommandLine由来でも明示flagと一致する(t *testing.T) {
	explicit, err := parseConfig([]string{
		"--controller-url=https://controller.test.walnuts.dev",
		"--operation-uid=operation-uid",
		"--host-uid=host-uid",
		"--boot-mac-address=00:11:22:33:44:55",
		"--plan-key-id=test-key",
		"--plan-key-file=/trust/plan.pem",
		"--preflight-only",
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}

	fromKernel := parseConfigFromKernelArguments(t, []string{
		"tart.agent.controller-url=https://controller.test.walnuts.dev",
		"tart.agent.operation-uid=operation-uid",
		"tart.agent.host-uid=host-uid",
		"tart.agent.boot-mac=00:11:22:33:44:55",
	})

	got := buildRegisterRequest(fromKernel, "system-uuid", "agent-instance", sampleRegisterDevices())
	want := buildRegisterRequest(explicit, "system-uuid", "agent-instance", sampleRegisterDevices())
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("kernel command line request = %#v, want %#v", got, want)
	}
}

func TestBuildRegisterRequestはVirtualMediaとHTTPBootPXEで一致する(t *testing.T) {
	kernelArgs, err := agentboot.KernelParameters{
		ControllerURL: "https://controller.test.walnuts.dev",
		HostUID:       "host-uid",
		OperationUID:  "operation-uid",
		BootMAC:       "AA-BB-CC-DD-EE-FF",
		TrustURL:      "https://artifacts.test.walnuts.dev/agent",
	}.Arguments()
	if err != nil {
		t.Fatalf("KernelParameters.Arguments() error = %v", err)
	}
	script, err := agentboot.BuildScript(agentboot.ScriptInput{
		ArtifactBaseURL: "https://artifacts.test.walnuts.dev/agent",
		AgentAPIURL:     "https://controller.test.walnuts.dev",
		ArtifactDigest:  "sha256:" + strings.Repeat("a", 64),
		HostUID:         "host-uid",
		OperationUID:    "operation-uid",
		BootMACAddress:  "AA-BB-CC-DD-EE-FF",
	})
	if err != nil {
		t.Fatalf("agentboot.BuildScript() error = %v", err)
	}

	fromVirtualMedia := parseConfigFromKernelArguments(t, extractAgentKernelArgumentsFromGRUBConfig(t, grubConfigForKernelArguments(kernelArgs)))
	fromHTTPBoot := parseConfigFromKernelArguments(t, extractAgentKernelArgumentsFromIPXEScript(t, script))

	got := buildRegisterRequest(fromVirtualMedia, "system-uuid", "agent-instance", sampleRegisterDevices())
	want := buildRegisterRequest(fromHTTPBoot, "system-uuid", "agent-instance", sampleRegisterDevices())
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("VirtualMedia request = %#v, HTTPBoot/PXE request = %#v", got, want)
	}
}

func swapKernelCommandLineReader(reader func() ([]byte, error)) func() {
	previous := readKernelCommandLine
	readKernelCommandLine = reader
	return func() {
		readKernelCommandLine = previous
	}
}

func stringsJoinFields(fields ...string) string {
	return strings.Join(fields, " ")
}

func parseConfigFromKernelArguments(t *testing.T, kernelArguments []string) config {
	t.Helper()

	restore := swapKernelCommandLineReader(func() ([]byte, error) {
		return []byte(stringsJoinFields(kernelArguments...)), nil
	})
	defer restore()

	cfg, err := parseConfig([]string{
		"--plan-key-id=test-key",
		"--plan-key-file=/trust/plan.pem",
		"--preflight-only",
	})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	return cfg
}

func sampleRegisterDevices() []disk.Device {
	return []disk.Device{
		{
			Path:         "/dev/nvme0n1",
			ByIDPaths:    []string{"/dev/disk/by-id/nvme-test"},
			SerialNumber: "serial-1",
			WWN:          "wwn-1",
			SizeBytes:    1024,
			HoldsAgentOS: true,
		},
		{
			Path:         "/dev/sda",
			ByIDPaths:    []string{"/dev/disk/by-id/ata-test", "/dev/disk/by-id/wwn-test"},
			SerialNumber: "serial-2",
			WWN:          "wwn-2",
			SizeBytes:    2048,
			HoldsAgentOS: false,
		},
	}
}

func grubConfigForKernelArguments(kernelArguments []string) string {
	return strings.Join([]string{
		"set timeout=0",
		"menuentry 'Provisioning Agent' {",
		"\tlinux /vmlinuz initrd=/initrd " + strings.Join(kernelArguments, " "),
		"\tinitrd /initrd",
		"}",
	}, "\n")
}

func extractAgentKernelArgumentsFromGRUBConfig(t *testing.T, config string) []string {
	t.Helper()

	for line := range strings.SplitSeq(config, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "linux" {
			continue
		}

		var args []string
		for _, field := range fields[2:] {
			if isAgentKernelArgument(field) {
				args = append(args, field)
			}
		}
		return args
	}

	t.Fatal("linux line not found")
	return nil
}

func extractAgentKernelArgumentsFromIPXEScript(t *testing.T, script string) []string {
	t.Helper()

	for line := range strings.SplitSeq(script, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "kernel" {
			continue
		}

		var args []string
		for _, field := range fields[2:] {
			if isAgentKernelArgument(field) {
				args = append(args, field)
			}
		}
		return args
	}

	t.Fatal("kernel line not found")
	return nil
}

func isAgentKernelArgument(argument string) bool {
	for _, key := range agentboot.KernelParameterKeys() {
		if strings.HasPrefix(argument, key+"=") {
			return true
		}
	}
	return false
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
