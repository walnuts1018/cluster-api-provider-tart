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
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("qemuFirstBootDropIn() does not contain %q\n%s", want, got)
		}
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
