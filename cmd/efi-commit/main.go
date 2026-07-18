// Copyright 2026.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type config struct {
	bootDevice, stateDevice, dataDevice string
	osDevice, verityDevice              string
	osPartUUID, verityPartUUID          string
	slot, rootHash                      string
	kernel, initrd                      string
	tlsCAFile, planKeyFile              string
	systemdBoot                         string
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cfg, err := parse(args)
	if err != nil {
		return err
	}
	if err := validate(cfg); err != nil {
		return err
	}
	temporary, err := os.MkdirTemp("", "tart-efi-commit-")
	if err != nil {
		return fmt.Errorf("create mount directory: %w", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(temporary); removeErr != nil {
			slog.Warn("Failed to remove EFI commit temporary directory", "error", removeErr)
		}
	}() // マウント解除を完了してから一時ディレクトリを削除する。
	bootMount := filepath.Join(temporary, "boot")
	stateMount := filepath.Join(temporary, "state")
	if err := os.MkdirAll(bootMount, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(stateMount, 0o755); err != nil {
		return err
	}
	if err := command(ctx, "mkfs.ext4", "-F", "-L", "tart-state", cfg.stateDevice); err != nil {
		return fmt.Errorf("format State partition: %w", err)
	}
	if err := command(ctx, "mkfs.ext4", "-F", "-L", "tart-data", cfg.dataDevice); err != nil {
		return fmt.Errorf("format Data partition: %w", err)
	}
	if err := command(ctx, "mount", "-t", "vfat", cfg.bootDevice, bootMount); err != nil {
		return fmt.Errorf("mount Boot partition: %w", err)
	}
	bootMounted := true
	defer func() {
		if bootMounted {
			if unmountErr := command(context.Background(), "umount", bootMount); unmountErr != nil {
				slog.Error("Failed to unmount Boot partition", "error", unmountErr)
			}
		}
	}()
	if err := command(ctx, "mount", "-t", "ext4", cfg.stateDevice, stateMount); err != nil {
		return fmt.Errorf("mount State partition: %w", err)
	}
	stateMounted := true
	defer func() {
		if stateMounted {
			if unmountErr := command(context.Background(), "umount", stateMount); unmountErr != nil {
				slog.Error("Failed to unmount State partition", "error", unmountErr)
			}
		}
	}()
	if err := installBoot(ctx, cfg, bootMount); err != nil {
		return err
	}
	if err := installTrust(ctx, cfg, stateMount); err != nil {
		return err
	}
	if err := command(ctx, "sync"); err != nil {
		return fmt.Errorf("sync committed boot files: %w", err)
	}
	return nil
}

func installBoot(ctx context.Context, cfg config, mountPoint string) error {
	entries := filepath.Join(mountPoint, "loader", "entries")
	if err := os.MkdirAll(entries, 0o755); err != nil {
		return fmt.Errorf("create systemd-boot entries directory: %w", err)
	}
	if err := command(ctx, "install", "-D", "-m", "0644", cfg.systemdBoot, filepath.Join(mountPoint, "EFI", "BOOT", "BOOTX64.EFI")); err != nil {
		return fmt.Errorf("install systemd-boot: %w", err)
	}
	for _, payload := range []struct{ source, name string }{{cfg.kernel, "vmlinuz"}, {cfg.initrd, "initrd"}} {
		if err := command(ctx, "install", "-D", "-m", "0644", payload.source, filepath.Join(mountPoint, payload.name)); err != nil {
			return fmt.Errorf("install %s: %w", payload.name, err)
		}
	}
	cmdline, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return fmt.Errorf("read provisioning kernel command line: %w", err)
	}
	options := strings.TrimSpace(string(cmdline)) + " root=PARTUUID=" + cfg.osPartUUID + " ro systemd.verity=1 verity.root-hash=" + cfg.rootHash
	entry := strings.Join([]string{
		"title Tart OS " + cfg.slot,
		"version 1",
		"linux /vmlinuz",
		"initrd /initrd",
		"options " + options,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(entries, "tart-os-"+strings.ToLower(cfg.slot)+".conf"), []byte(entry), 0o644); err != nil {
		return fmt.Errorf("write systemd-boot entry: %w", err)
	}
	loader := "default tart-os-*\ntimeout 0\neditor no\n"
	if err := os.WriteFile(filepath.Join(mountPoint, "loader", "loader.conf"), []byte(loader), 0o644); err != nil {
		return fmt.Errorf("write systemd-boot loader configuration: %w", err)
	}
	return nil
}

func installTrust(ctx context.Context, cfg config, stateMount string) error {
	trustDir := filepath.Join(stateMount, "tart", "trust")
	if err := os.MkdirAll(trustDir, 0o700); err != nil {
		return fmt.Errorf("create State trust directory: %w", err)
	}
	for _, source := range []string{cfg.tlsCAFile, cfg.planKeyFile} {
		if err := command(ctx, "install", "-D", "-m", "0600", source, filepath.Join(trustDir, filepath.Base(source))); err != nil {
			return fmt.Errorf("persist controller trust %s: %w", filepath.Base(source), err)
		}
	}
	return nil
}

func parse(args []string) (config, error) {
	var c config
	flags := flag.NewFlagSet("efi-commit", flag.ContinueOnError)
	flags.StringVar(&c.bootDevice, "boot-device", "", "Boot partition device")
	flags.StringVar(&c.stateDevice, "state-device", "", "State partition device")
	flags.StringVar(&c.dataDevice, "data-device", "", "Data partition device")
	flags.StringVar(&c.osDevice, "os-device", "", "OS partition device")
	flags.StringVar(&c.verityDevice, "verity-device", "", "Verity partition device")
	flags.StringVar(&c.osPartUUID, "os-partuuid", "", "OS partition UUID")
	flags.StringVar(&c.verityPartUUID, "verity-partuuid", "", "Verity partition UUID")
	flags.StringVar(&c.slot, "slot", "", "OS slot")
	flags.StringVar(&c.rootHash, "root-hash", "", "dm-verity root hash")
	flags.StringVar(&c.kernel, "kernel", "", "Kernel payload")
	flags.StringVar(&c.initrd, "initrd", "", "Initrd payload")
	flags.StringVar(&c.tlsCAFile, "tls-ca-file", "", "Controller CA certificate")
	flags.StringVar(&c.planKeyFile, "plan-key-file", "", "Agent Plan public key")
	flags.StringVar(&c.systemdBoot, "systemd-boot", "/usr/lib/systemd/boot/efi/systemd-bootx64.efi", "systemd-boot EFI binary")
	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	if flags.NArg() != 0 {
		return config{}, errors.New("unexpected positional arguments")
	}
	return c, nil
}

func validate(c config) error {
	for name, value := range map[string]string{
		"boot-device": c.bootDevice, "state-device": c.stateDevice, "data-device": c.dataDevice,
		"os-device": c.osDevice, "verity-device": c.verityDevice, "os-partuuid": c.osPartUUID,
		"verity-partuuid": c.verityPartUUID, "slot": c.slot, "root-hash": c.rootHash,
		"kernel": c.kernel, "initrd": c.initrd, "tls-ca-file": c.tlsCAFile, "plan-key-file": c.planKeyFile,
	} {
		if value == "" {
			return fmt.Errorf("--%s is required", name)
		}
	}
	if c.slot != "A" && c.slot != "B" {
		return fmt.Errorf("--slot must be A or B, got %q", c.slot)
	}
	for _, path := range []string{c.bootDevice, c.stateDevice, c.dataDevice, c.osDevice, c.verityDevice, c.kernel, c.initrd, c.tlsCAFile, c.planKeyFile, c.systemdBoot} {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("required path %q: %w", path, err)
		}
	}
	return nil
}

func command(ctx context.Context, name string, args ...string) error {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return nil
}
