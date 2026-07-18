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

	"github.com/walnuts1018/cluster-api-provider-tart/artifact"
)

func TestRunGeneratesSignedManifestFromPayloads(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	keyPath := filepath.Join(root, "signing-key.pem")
	outputDir := filepath.Join(root, "output")
	imagePath := writeTestFile(t, root, "os.img", []byte("os filesystem"))
	verityPath := writeTestFile(t, root, "os.verity", []byte("verity tree"))
	kernelPath := writeTestFile(t, root, "vmlinuz", []byte("kernel"))
	initrdPath := writeTestFile(t, root, "initrd", []byte("initrd"))

	configJSON, err := json.Marshal(config{
		OS:              artifact.OS{Family: "ubuntu", Version: "24.04"},
		Architecture:    "amd64",
		Filesystem:      "ext4",
		StateSchema:     artifact.StateSchema{Min: 1, Max: 1},
		Kubernetes:      artifact.Kubernetes{Distribution: "kubeadm", LifecycleRuntime: "kubeadm.cluster.x-k8s.io/v1", Version: "v1.36.0"},
		Requirements:    artifact.Requirements{CPULevel: "x86-64-v1"},
		Generation:      1,
		PlatformProfile: "amd64-uefi-ab-ubuntu-24.04-kubeadm/v1",
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := os.WriteFile(configPath, configJSON, 0o644); err != nil {
		t.Fatalf("os.WriteFile(config) error = %v", err)
	}

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
		configPath:     configPath,
		imagePath:      imagePath,
		verityPath:     verityPath,
		verityRootHash: strings.Repeat("a", 64),
		kernelPath:     kernelPath,
		initrdPath:     initrdPath,
		signingKeyPath: keyPath,
		keyID:          "test-release-key",
		outputDir:      outputDir,
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}

	manifestData, err := os.ReadFile(filepath.Join(outputDir, "manifest.json"))
	if err != nil {
		t.Fatalf("os.ReadFile(manifest) error = %v", err)
	}
	manifest, err := artifact.Parse(manifestData)
	if err != nil {
		t.Fatalf("artifact.Parse() error = %v", err)
	}
	if manifest.Value().Image.SizeBytes != int64(len("os filesystem")) {
		t.Fatalf("manifest image size = %d", manifest.Value().Image.SizeBytes)
	}

	signatureData, err := os.ReadFile(filepath.Join(outputDir, "manifest.signature.json"))
	if err != nil {
		t.Fatalf("os.ReadFile(signature) error = %v", err)
	}
	var signature artifact.Signature
	if err := json.Unmarshal(signatureData, &signature); err != nil {
		t.Fatalf("json.Unmarshal(signature) error = %v", err)
	}
	if err := artifact.VerifySignature(
		manifest,
		signature,
		artifact.StaticTrustStore{"test-release-key": publicKey},
	); err != nil {
		t.Fatalf("artifact.VerifySignature() error = %v", err)
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
