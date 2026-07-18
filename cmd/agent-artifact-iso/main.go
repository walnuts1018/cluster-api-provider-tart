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
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	agentboot "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/agentboot"
)

type options struct {
	kernelPath      string
	initrdPath      string
	outputPath      string
	controllerURL   string
	hostUID         string
	operationUID    string
	bootMACAddress  string
	sourceDateEpoch string
}

type commandInvocation struct {
	name string
	args []string
	env  []string
}

type commandRunner func(commandInvocation) error

func main() {
	if err := run(parseFlags(), execRunner); err != nil {
		fmt.Fprintf(os.Stderr, "agent Artifact virtual media generation failed: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() options {
	var opts options
	flag.StringVar(&opts.kernelPath, "kernel", "", "Path to Agent kernel payload")
	flag.StringVar(&opts.initrdPath, "initrd", "", "Path to Agent initrd payload")
	flag.StringVar(&opts.outputPath, "output", "", "Path to write virtual-media.iso")
	flag.StringVar(&opts.controllerURL, "controller-url", "", "HTTPS Agent boot server base URL")
	flag.StringVar(&opts.hostUID, "host-uid", "", "TartHost UID passed to the Agent")
	flag.StringVar(&opts.operationUID, "operation-uid", "", "TartHostOperation UID passed to the Agent")
	flag.StringVar(&opts.bootMACAddress, "boot-mac", "", "Boot NIC MAC address passed to the Agent")
	flag.StringVar(&opts.sourceDateEpoch, "source-date-epoch", os.Getenv("SOURCE_DATE_EPOCH"), "Reproducible build timestamp")
	flag.Parse()
	return opts
}

func run(opts options, runner commandRunner) error {
	if runner == nil {
		return errors.New("command runner is required")
	}
	if err := validateOptions(opts); err != nil {
		return err
	}
	stagingDir, err := os.MkdirTemp(filepath.Dir(opts.outputPath), ".agent-iso-*")
	if err != nil {
		return fmt.Errorf("create ISO staging directory: %w", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(stagingDir); removeErr != nil {
			fmt.Fprintf(os.Stderr, "failed to remove ISO staging directory: %v\n", removeErr)
		}
	}()

	if err := copyFile(opts.kernelPath, filepath.Join(stagingDir, "vmlinuz"), 0o644); err != nil {
		return fmt.Errorf("stage kernel: %w", err)
	}
	if err := copyFile(opts.initrdPath, filepath.Join(stagingDir, "initrd"), 0o644); err != nil {
		return fmt.Errorf("stage initrd: %w", err)
	}
	grubDir := filepath.Join(stagingDir, "boot", "grub")
	if err := os.MkdirAll(grubDir, 0o755); err != nil {
		return fmt.Errorf("create GRUB directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(grubDir, "grub.cfg"), []byte(grubConfig(opts)), 0o644); err != nil {
		return fmt.Errorf("write GRUB config: %w", err)
	}

	env := os.Environ()
	if opts.sourceDateEpoch != "" {
		env = append(env, "SOURCE_DATE_EPOCH="+opts.sourceDateEpoch)
	}
	if err := runner(commandInvocation{
		name: "grub-mkrescue",
		args: []string{"-o", opts.outputPath, stagingDir, "--"},
		env:  env,
	}); err != nil {
		return fmt.Errorf("run grub-mkrescue: %w", err)
	}
	if info, err := os.Stat(opts.outputPath); err != nil {
		return fmt.Errorf("stat generated ISO: %w", err)
	} else if !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("generated ISO is empty or not a regular file")
	}
	return nil
}

func validateOptions(opts options) error {
	required := []struct {
		name  string
		value string
	}{
		{name: "kernel", value: opts.kernelPath},
		{name: "initrd", value: opts.initrdPath},
		{name: "output", value: opts.outputPath},
		{name: "controller-url", value: opts.controllerURL},
		{name: "host-uid", value: opts.hostUID},
		{name: "operation-uid", value: opts.operationUID},
		{name: "boot-mac", value: opts.bootMACAddress},
	}
	for _, item := range required {
		if item.value == "" {
			return fmt.Errorf("-%s is required", item.name)
		}
	}
	parsed, err := url.Parse(opts.controllerURL)
	if err != nil {
		return fmt.Errorf("parse controller URL: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("controller URL must be an HTTPS origin or base path without credentials, query, or fragment")
	}
	if _, err := net.ParseMAC(opts.bootMACAddress); err != nil {
		return fmt.Errorf("parse boot MAC address: %w", err)
	}
	for _, item := range []struct {
		name  string
		value string
	}{
		{name: "host-uid", value: opts.hostUID},
		{name: "operation-uid", value: opts.operationUID},
	} {
		if strings.ContainsAny(item.value, " \t\r\n\"'\\") {
			return fmt.Errorf("-%s contains characters that cannot be passed as a kernel argument", item.name)
		}
	}
	if opts.sourceDateEpoch != "" && strings.ContainsAny(opts.sourceDateEpoch, " \t\r\n") {
		return errors.New("source-date-epoch cannot contain whitespace")
	}
	return nil
}

func grubConfig(opts options) string {
	args, err := agentboot.KernelParameters{
		ControllerURL: opts.controllerURL,
		HostUID:       opts.hostUID,
		OperationUID:  opts.operationUID,
		BootMAC:       opts.bootMACAddress,
	}.Arguments()
	if err != nil {
		// validateOptions が先に拒否するため、ここへ到達するのは将来の回帰だけである。
		panic(err)
	}
	return "set timeout=0\n" +
		"set default=0\n" +
		"menuentry 'Tart Provisioning Agent' {\n" +
		"  linux /vmlinuz " + strings.Join(args, " ") + "\n" +
		"  initrd /initrd\n" +
		"}\n"
}

func copyFile(source, target string, mode os.FileMode) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return errors.New("source file is empty")
	}
	return os.WriteFile(target, data, mode)
}

func execRunner(invocation commandInvocation) error {
	command := exec.CommandContext(context.Background(), invocation.name, invocation.args...)
	command.Env = invocation.env
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}
