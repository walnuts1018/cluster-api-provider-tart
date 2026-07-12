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
	"strings"
	"testing"
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
		"tart.agent.controller-url=https://controller.test/agent",
		"tart.agent.host-uid=host-uid",
		"tart.agent.operation-uid=operation-uid",
		"tart.agent.boot-mac=aa:bb:cc:dd:ee:ff",
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

func writeTestFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("os.WriteFile(%s) error = %v", name, err)
	}
	return path
}
