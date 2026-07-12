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
	"testing"
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
