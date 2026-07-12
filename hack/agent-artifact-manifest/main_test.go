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
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentartifact"
)

func TestRunGeneratesSignedAgentArtifactManifestWithVirtualMedia(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	keyPath := filepath.Join(root, "signing-key.pem")
	outputDir := filepath.Join(root, "output")
	kernelPath := writeTestFile(t, root, "vmlinuz", []byte("kernel"))
	initrdPath := writeTestFile(t, root, "initrd", []byte("initrd"))
	mediaPath := writeTestFile(t, root, "virtual-media.iso", []byte("iso"))

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey() error = %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("x509.MarshalPKCS8PrivateKey() error = %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("os.WriteFile(key) error = %v", err)
	}

	err = run(options{
		reference:        "oci://registry.test/agent@sha256:" + strings.Repeat("a", 64),
		kernelPath:       kernelPath,
		initrdPath:       initrdPath,
		virtualMediaPath: mediaPath,
		signingKeyPath:   keyPath,
		keyID:            "agent-release",
		outputDir:        outputDir,
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}

	manifestData, err := os.ReadFile(filepath.Join(outputDir, "manifest.json"))
	if err != nil {
		t.Fatalf("os.ReadFile(manifest) error = %v", err)
	}
	manifest, err := agentartifact.Parse(manifestData)
	if err != nil {
		t.Fatalf("agentartifact.Parse() error = %v", err)
	}
	if manifest.Value().VirtualMedia == nil {
		t.Fatal("manifest.VirtualMedia = nil, want descriptor")
	}
	if manifest.Value().VirtualMedia.SizeBytes != int64(len("iso")) {
		t.Fatalf("virtualMedia size = %d", manifest.Value().VirtualMedia.SizeBytes)
	}

	signatureData, err := os.ReadFile(filepath.Join(outputDir, "manifest.signature.json"))
	if err != nil {
		t.Fatalf("os.ReadFile(signature) error = %v", err)
	}
	var signature agentartifact.Signature
	if err := json.Unmarshal(signatureData, &signature); err != nil {
		t.Fatalf("json.Unmarshal(signature) error = %v", err)
	}
	if err := agentartifact.VerifySignature(
		manifest,
		signature,
		agentartifact.StaticTrustStore{"agent-release": publicKey},
	); err != nil {
		t.Fatalf("agentartifact.VerifySignature() error = %v", err)
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
