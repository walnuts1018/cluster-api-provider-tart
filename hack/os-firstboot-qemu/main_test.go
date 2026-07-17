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
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

func TestBuildKernelCommandLineはfirstbootAgent入力を揃える(t *testing.T) {
	got := buildKernelCommandLine("https://10.0.2.2:8443")
	fields := strings.Fields(got)

	for _, want := range []string{
		"root=/dev/vda",
		"ro",
		"console=ttyS0",
		"ip=dhcp",
		"tart.agent.controller-url=https://10.0.2.2:8443",
		"tart.agent.operation-uid=" + operationUID,
		"tart.agent.host-uid=" + hostUID,
		"tart.agent.boot-mac=" + bootMAC,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("buildKernelCommandLine() = %q, want substring %q", got, want)
		}
	}
	if slices.Contains(fields, "rw") {
		t.Fatalf("buildKernelCommandLine() = %q, want read-only root flags only", got)
	}
}

func TestGenerateServerCertificateはQEMUGuest向けSANを含む(t *testing.T) {
	workDir := t.TempDir()

	_, certPath, err := generateServerCertificate(workDir)
	if err != nil {
		t.Fatalf("generateServerCertificate() error = %v", err)
	}

	pemData, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	block, _ := pem.Decode(pemData)
	if block == nil {
		t.Fatal("PEM certificate block was not found")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("x509.ParseCertificate() error = %v", err)
	}
	if certificate.Subject.CommonName != "10.0.2.2" {
		t.Fatalf("certificate CommonName = %q, want 10.0.2.2", certificate.Subject.CommonName)
	}
	if !containsIP(certificate.IPAddresses, net.ParseIP("10.0.2.2")) {
		t.Fatalf("certificate SAN IPs = %v, want 10.0.2.2", certificate.IPAddresses)
	}
	if !containsIP(certificate.IPAddresses, net.ParseIP("127.0.0.1")) {
		t.Fatalf("certificate SAN IPs = %v, want 127.0.0.1", certificate.IPAddresses)
	}
	if !containsString(certificate.DNSNames, "localhost") {
		t.Fatalf("certificate SAN DNS names = %v, want localhost", certificate.DNSNames)
	}
}

func TestBuildSignedPlanはvirtioRootDiskIdentityとBootstrapTargetを固定する(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey() error = %v", err)
	}
	deadline := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	signedPlan, digest, err := buildSignedPlan(privateKey, 1<<20, deadline)
	if err != nil {
		t.Fatalf("buildSignedPlan() error = %v", err)
	}

	if signedPlan.Plan.RootDevice.DeviceName != "/dev/disk/by-id/virtio-"+targetDiskSerial {
		t.Fatalf("RootDevice.DeviceName = %q", signedPlan.Plan.RootDevice.DeviceName)
	}
	if signedPlan.Plan.RootDevice.SerialNumber != targetDiskSerial {
		t.Fatalf("RootDevice.SerialNumber = %q", signedPlan.Plan.RootDevice.SerialNumber)
	}
	if signedPlan.Plan.Deadline != deadline {
		t.Fatalf("Plan.Deadline = %s, want %s", signedPlan.Plan.Deadline, deadline)
	}
	if signedPlan.Plan.Bootstrap == nil || signedPlan.Plan.Bootstrap.MachineUID != machineUID {
		t.Fatalf("Plan.Bootstrap = %#v", signedPlan.Plan.Bootstrap)
	}
	if signedPlan.Signature.KeyID != planKeyID {
		t.Fatalf("Signature.KeyID = %q, want %q", signedPlan.Signature.KeyID, planKeyID)
	}

	validated, err := agentprotocol.ValidatePlan(signedPlan.Plan)
	if err != nil {
		t.Fatalf("ValidatePlan() error = %v", err)
	}
	gotDigest, err := validated.Digest()
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	if gotDigest.String() != digest {
		t.Fatalf("Plan digest = %q, want %q", gotDigest.String(), digest)
	}
}

func TestQEMUDiskSerialはVirtioBlkIdentityに収まる(t *testing.T) {
	for _, serial := range []string{rootDiskSerial, targetDiskSerial} {
		if len(serial) > 20 {
			t.Fatalf("serial %q length = %d, want <= 20", serial, len(serial))
		}
	}
}

func TestParseConfigは既定値を持つ(t *testing.T) {
	cfg, err := parseConfig(nil)
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.artifactDir != defaultArtifactDir {
		t.Fatalf("artifactDir = %q, want %q", cfg.artifactDir, defaultArtifactDir)
	}
	if cfg.timeout != defaultTimeout {
		t.Fatalf("timeout = %s, want %s", cfg.timeout, defaultTimeout)
	}
	if cfg.cpu != defaultCPU {
		t.Fatalf("cpu = %q, want %q", cfg.cpu, defaultCPU)
	}
	if cfg.scenario != defaultScenario {
		t.Fatalf("scenario = %q, want %q", cfg.scenario, defaultScenario)
	}
}

func TestParseConfigは未知のScenarioを拒否する(t *testing.T) {
	if _, err := parseConfig([]string{"--scenario", "unknown"}); err == nil {
		t.Fatal("parseConfig() accepted an unknown scenario")
	}
}

func TestBootEntrySelectionFromLogは選択されたEntryを読む(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serial.log")
	if err := os.WriteFile(path, []byte(serialMarkerBootEntrySelected+"rollback\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	got, ok := bootEntrySelectionFromLog(path)
	if !ok {
		t.Fatal("bootEntrySelectionFromLog() = not found, want rollback marker")
	}
	if got.SelectedEntry != "rollback" {
		t.Fatalf("SelectedEntry = %q, want rollback", got.SelectedEntry)
	}
}

func TestBootTrialMetadataWriteFromDiskはゼロ埋めrawからJSONを読む(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boot-metadata.raw")
	recordJSON := mustJSON(bootTrialMetadataRecord{
		ActiveSlot:         "B",
		TargetSlot:         "B",
		RollbackSlot:       "A",
		ArtifactGeneration: 2,
		RemainingAttempts:  2,
	})
	data := append([]byte(recordJSON), make([]byte, 256-len(recordJSON))...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	got, ok := bootTrialMetadataWriteFromDisk(path)
	if !ok {
		t.Fatal("bootTrialMetadataWriteFromDisk() = not found, want persisted record")
	}
	if got.Record.ActiveSlot != "B" || got.Record.RemainingAttempts != 2 {
		t.Fatalf("bootTrialMetadataWriteFromDisk() = %+v", got.Record)
	}
}

func TestWriteTextFileは内容を書き出す(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.txt")

	gotPath, err := writeTextFile(path, "hello\n")
	if err != nil {
		t.Fatalf("writeTextFile() error = %v", err)
	}
	if gotPath != path {
		t.Fatalf("writeTextFile() path = %q, want %q", gotPath, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("file contents = %q", data)
	}
}

func TestQEMUFirstBootDropInはReadOnlyRoot向けの一時Stateを使う(t *testing.T) {
	got := qemuFirstBootDropIn()

	for _, want := range []string{
		"[Service]",
		"Environment=TART_STATE_DIR=/run/tart/state",
		"Environment=TART_BOOTSTRAP_ADAPTER=/bin/true",
		"Environment=TART_SYSTEM_UUID=00000000-0000-4000-8000-000000000001",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("qemuFirstBootDropIn() does not contain %q\n%s", want, got)
		}
	}
}

func TestVerifyBootReportはReadOnlyRoot起動後の基本状態を受け入れる(t *testing.T) {
	bootstrapDigest := "sha256:" + strings.Repeat("a", 64)

	err := verifyBootReport(agentprotocol.BootReportRequest{
		APIVersion:             agentprotocol.APIVersion,
		OperationUID:           operationUID,
		PlanDigest:             "sha256:" + strings.Repeat("b", 64),
		BootID:                 "boot-1",
		MachineID:              "machine-1",
		ActiveSlot:             activeSlot,
		ArtifactGeneration:     1,
		StateMounted:           true,
		DataMounted:            true,
		BootstrapApplied:       true,
		BootstrapPayloadDigest: bootstrapDigest,
	}, bootstrapDigest)
	if err != nil {
		t.Fatalf("verifyBootReport() error = %v", err)
	}
}

func TestRootObservationFromLogはReadOnlyRoot証跡を読む(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serial.log")
	logText := strings.Join([]string{
		serialMarkerRootSource + "/dev/vda",
		serialMarkerRootOptions + "ro,relatime",
		serialMarkerRootReadOnly + "true",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(logText), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	got, hasSource, hasReadOnly := rootObservationFromLog(path)
	if !hasSource || !hasReadOnly {
		t.Fatalf("rootObservationFromLog() flags = (%t, %t), want true, true", hasSource, hasReadOnly)
	}
	if got.Source != "/dev/vda" || got.Options != "ro,relatime" || !got.MountedReadOnly {
		t.Fatalf("rootObservationFromLog() = %#v", got)
	}
}

func TestReadLogTailは直近80行を返す(t *testing.T) {
	lines := make([]string, 0, 90)
	for i := range 90 {
		lines = append(lines, "line-"+strconv.Itoa(i))
	}
	path := filepath.Join(t.TempDir(), "serial.log")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	got, err := readLogTail(path)
	if err != nil {
		t.Fatalf("readLogTail() error = %v", err)
	}
	if strings.Contains(got, "line-9") {
		t.Fatalf("readLogTail() contains dropped prefix: %q", got)
	}
	if !strings.Contains(got, "line-10") || !strings.Contains(got, "line-89") {
		t.Fatalf("readLogTail() = %q, want line-10 through line-89", got)
	}
}

func TestQEMUBootTrialMetadataScriptは期待する永続化対象を固定する(t *testing.T) {
	got := qemuBootTrialMetadataScript()

	for _, want := range []string{
		metadataDiskSerial,
		serialMarkerBootMetadataWritten,
		serialMarkerBootMetadataRead,
		`"activeSlot":"B"`,
		`"rollbackSlot":"A"`,
		`"remainingAttempts":2`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("qemuBootTrialMetadataScript() does not contain %q\n%s", want, got)
		}
	}
}

func TestQEMUBootloaderRollbackScriptはKernelCmdlineから選択Entryを出力する(t *testing.T) {
	got := qemuBootloaderRollbackScript()

	for _, want := range []string{
		serialMarkerBootEntrySelected,
		"tart.qemu.boot-entry=target",
		"tart.qemu.boot-entry=rollback",
		"systemctl poweroff --force --force",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("qemuBootloaderRollbackScript() does not contain %q\n%s", want, got)
		}
	}
}

func TestBootloaderEntryConfigは指定rootと識別子を埋め込む(t *testing.T) {
	got := bootloaderEntryConfig("Target", "2", "/vmlinuz", "/initrd", "/dev/vdb", "target")

	for _, want := range []string{
		"title Target",
		"version 2",
		"linux /vmlinuz",
		"initrd /initrd",
		"options root=/dev/vdb ro console=ttyS0 tart.qemu.boot-entry=target",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("bootloaderEntryConfig() does not contain %q\n%s", want, got)
		}
	}
}

func TestBootTrialMetadataFromLogはWriteとReadのJSONを読む(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serial.log")
	recordJSON := mustJSON(bootTrialMetadataRecord{
		ActiveSlot:         "B",
		TargetSlot:         "B",
		RollbackSlot:       "A",
		ArtifactGeneration: 2,
		RemainingAttempts:  2,
	})
	logText := strings.Join([]string{
		serialMarkerBootMetadataWritten + recordJSON,
		serialMarkerBootMetadataSynced + "true",
		serialMarkerBootMetadataRead + recordJSON,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(logText), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	written, synced := bootTrialMetadataWriteFromLog(path)
	if !synced {
		t.Fatal("bootTrialMetadataWriteFromLog() did not detect written metadata")
	}
	read, ok := bootTrialMetadataReadFromLog(path)
	if !ok {
		t.Fatal("bootTrialMetadataReadFromLog() did not detect persisted metadata")
	}
	if written.Record != read.Record {
		t.Fatalf("metadata mismatch: written=%#v read=%#v", written.Record, read.Record)
	}
}

func TestWaitForBootTrialMetadataAfterPowerLossはmetadataDisk中心で永続化証跡を読む(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	workDir := t.TempDir()
	serialLogPath := filepath.Join(workDir, "serial.log")
	metadataDiskPath := filepath.Join(workDir, "boot-metadata.raw")

	if err := os.WriteFile(serialLogPath, []byte(strings.Join([]string{
		serialMarkerRootSource + "/dev/vda",
		serialMarkerRootOptions + "ro,relatime",
		serialMarkerRootReadOnly + "true",
		"",
	}, "\n")), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	recordJSON := mustJSON(bootTrialMetadataRecord{
		ActiveSlot:         "B",
		TargetSlot:         "B",
		RollbackSlot:       "A",
		ArtifactGeneration: 2,
		RemainingAttempts:  2,
	})
	data := append([]byte(recordJSON), make([]byte, 256-len(recordJSON))...)
	if err := os.WriteFile(metadataDiskPath, data, 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	got, err := waitForBootTrialMetadataAfterPowerLoss(ctx, serialLogPath, metadataDiskPath)
	if err != nil {
		t.Fatalf("waitForBootTrialMetadataAfterPowerLoss() error = %v", err)
	}
	if got.Record.ActiveSlot != "B" || got.Record.RemainingAttempts != 2 {
		t.Fatalf("waitForBootTrialMetadataAfterPowerLoss() = %+v", got.Record)
	}
}

func containsIP(addresses []net.IP, target net.IP) bool {
	for _, address := range addresses {
		if address.Equal(target) {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	return slices.Contains(values, target)
}
