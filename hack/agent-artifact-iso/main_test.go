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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	agentboot "github.com/walnuts1018/cluster-api-provider-tart/internal/domain/agentboot"
)

func TestRunStagesGrubISOInputs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	kernelPath := writeTestFile(t, root, "vmlinuz", []byte("kernel"))
	initrdPath := writeTestFile(t, root, "initrd", []byte("initrd"))
	outputPath := filepath.Join(root, "virtual-media.iso")

	var captured commandInvocation
	var config string
	err := run(options{
		kernelPath:      kernelPath,
		initrdPath:      initrdPath,
		outputPath:      outputPath,
		controllerURL:   "https://controller.test/agent",
		hostUID:         "host-uid",
		operationUID:    "operation-uid",
		bootMACAddress:  "aa:bb:cc:dd:ee:ff",
		sourceDateEpoch: "1798761600",
	}, func(invocation commandInvocation) error {
		captured = invocation
		if invocation.name != "grub-mkrescue" {
			t.Fatalf("command = %q, want grub-mkrescue", invocation.name)
		}
		kernelData, err := os.ReadFile(filepath.Join(invocation.args[2], "vmlinuz"))
		if err != nil {
			t.Fatalf("os.ReadFile(staged kernel) error = %v", err)
		}
		if string(kernelData) != "kernel" {
			t.Fatalf("staged kernel = %q", kernelData)
		}
		grubConfig, err := os.ReadFile(filepath.Join(invocation.args[2], "boot", "grub", "grub.cfg"))
		if err != nil {
			t.Fatalf("os.ReadFile(grub.cfg) error = %v", err)
		}
		config = string(grubConfig)
		if err := os.WriteFile(outputPath, []byte("iso"), 0o644); err != nil {
			t.Fatalf("os.WriteFile(output) error = %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}

	if len(captured.args) != 4 || captured.args[0] != "-o" || captured.args[1] != outputPath {
		t.Fatalf("grub-mkrescue args = %v, want -o output staging", captured.args)
	}
	for _, want := range []string{
		"linux /vmlinuz",
		"initrd /initrd",
		agentboot.KernelParameterControllerURL + "=https://controller.test/agent",
		agentboot.KernelParameterHostUID + "=host-uid",
		agentboot.KernelParameterOperationUID + "=operation-uid",
		agentboot.KernelParameterBootMAC + "=aa:bb:cc:dd:ee:ff",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("grub.cfg does not contain %q:\n%s", want, config)
		}
	}
}

func TestRunRejectsSecretBearingKernelArguments(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	err := run(options{
		kernelPath:     writeTestFile(t, root, "vmlinuz", []byte("kernel")),
		initrdPath:     writeTestFile(t, root, "initrd", []byte("initrd")),
		outputPath:     filepath.Join(root, "virtual-media.iso"),
		controllerURL:  "https://controller.test/agent?token=secret",
		hostUID:        "host-uid",
		operationUID:   "operation-uid",
		bootMACAddress: "aa:bb:cc:dd:ee:ff",
	}, func(commandInvocation) error {
		t.Fatal("runner should not be called")
		return nil
	})
	if err == nil {
		t.Fatal("run() error = nil, want validation error")
	}
}

func TestVirtualMediaはHTTPBootとPXEと同じregister入力へ収束する(t *testing.T) {
	t.Parallel()

	opts := options{
		controllerURL:  "https://controller.test/agent",
		hostUID:        "host-uid",
		operationUID:   "operation-uid",
		bootMACAddress: "AA-BB-CC-DD-EE-FF",
	}
	script, err := agentboot.BuildScript(agentboot.ScriptInput{
		ArtifactBaseURL: "https://artifacts.test/agent",
		AgentAPIURL:     opts.controllerURL,
		ArtifactDigest:  "sha256:" + strings.Repeat("c", 64),
		HostUID:         opts.hostUID,
		OperationUID:    opts.operationUID,
		BootMACAddress:  opts.bootMACAddress,
	})
	if err != nil {
		t.Fatalf("agentboot.BuildScript() error = %v", err)
	}

	gotVirtualMedia := extractAgentKernelArgumentsFromGRUBConfig(t, grubConfig(opts))
	gotNetworkBoot := extractAgentKernelArgumentsFromIPXEScript(t, script)
	want := []string{
		agentboot.KernelParameterBootMAC + "=aa:bb:cc:dd:ee:ff",
		agentboot.KernelParameterControllerURL + "=https://controller.test/agent",
		agentboot.KernelParameterHostUID + "=host-uid",
		agentboot.KernelParameterOperationUID + "=operation-uid",
	}
	slices.Sort(gotVirtualMedia)
	slices.Sort(gotNetworkBoot)
	if !slices.Equal(gotVirtualMedia, want) {
		t.Fatalf("VirtualMedia kernel args = %v, want %v", gotVirtualMedia, want)
	}
	if !slices.Equal(gotNetworkBoot, want) {
		t.Fatalf("HTTPBoot/PXE kernel args = %v, want %v", gotNetworkBoot, want)
	}
}

func writeTestFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("os.WriteFile(%s) error = %v", name, err)
	}
	return path
}

func extractAgentKernelArgumentsFromGRUBConfig(t *testing.T, config string) []string {
	t.Helper()

	for line := range strings.SplitSeq(config, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "linux" {
			continue
		}

		args := make([]string, 0, len(fields)-2)
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

		args := make([]string, 0, len(fields)-2)
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
