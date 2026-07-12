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

package agentartifact

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
)

func TestManifest署名とPayloadを検証する(t *testing.T) {
	kernel := []byte("agent-kernel")
	initrd := []byte("agent-initrd")
	validated, err := Validate(validManifest(kernel, initrd))
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	canonical, err := validated.CanonicalJSON()
	if err != nil {
		t.Fatalf("CanonicalJSON() error = %v", err)
	}
	parsed, err := Parse(append([]byte(" \n"), append(canonical, '\n')...))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	signature, err := Sign(parsed, "agent-release", privateKey)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if err := VerifySignature(parsed, signature, StaticTrustStore{"agent-release": publicKey}); err != nil {
		t.Fatalf("VerifySignature() error = %v", err)
	}
	if err := VerifyPayloads(parsed, bytes.NewReader(kernel), bytes.NewReader(initrd)); err != nil {
		t.Fatalf("VerifyPayloads() error = %v", err)
	}

	tampered := append([]byte(nil), initrd...)
	tampered[0] ^= 0xff
	if err := VerifyPayloads(parsed, bytes.NewReader(kernel), bytes.NewReader(tampered)); err == nil {
		t.Fatal("VerifyPayloads() accepted modified initrd")
	}
}

func TestManifestはVirtualMediaPayloadを検証する(t *testing.T) {
	media := []byte("agent-virtual-media")
	manifest := validManifest([]byte("kernel"), []byte("initrd"))
	descriptor := DescriptorFromBytes(media)
	manifest.VirtualMedia = &descriptor
	validated, err := Validate(manifest)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if err := VerifyVirtualMediaPayload(validated, bytes.NewReader(media)); err != nil {
		t.Fatalf("VerifyVirtualMediaPayload() error = %v", err)
	}
	if err := VerifyVirtualMediaPayload(validated, bytes.NewReader([]byte("changed"))); err == nil {
		t.Fatal("VerifyVirtualMediaPayload() accepted modified media")
	}
}

func TestValidateは不正なManifestを拒否する(t *testing.T) {
	kernel := []byte("agent-kernel")
	initrd := []byte("agent-initrd")
	tests := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{name: "可変OCI参照", mutate: func(manifest *Manifest) { manifest.Reference = "oci://registry.test/agent:latest" }},
		{name: "未対応architecture", mutate: func(manifest *Manifest) { manifest.Architecture = "arm64" }},
		{name: "未対応firmware", mutate: func(manifest *Manifest) { manifest.Firmware = "LegacyBIOS" }},
		{name: "未対応Profile", mutate: func(manifest *Manifest) { manifest.PlatformProfile = "amd64-uefi-ab/v2" }},
		{name: "不正なkernel digest", mutate: func(manifest *Manifest) { manifest.Kernel.Digest = "sha256:invalid" }},
		{name: "空のinitrd", mutate: func(manifest *Manifest) { manifest.Initrd.SizeBytes = 0 }},
		{name: "不正なVirtualMedia", mutate: func(manifest *Manifest) {
			descriptor := Descriptor{Digest: "sha256:invalid", SizeBytes: 1}
			manifest.VirtualMedia = &descriptor
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := validManifest(kernel, initrd)
			test.mutate(&manifest)
			if _, err := Validate(manifest); err == nil {
				t.Fatal("Validate() accepted invalid manifest")
			}
		})
	}
}

func TestVerifySignatureは変更と未信頼鍵を拒否する(t *testing.T) {
	validated, err := Validate(validManifest([]byte("kernel"), []byte("initrd")))
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	signature, err := Sign(validated, "agent-release", privateKey)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if err := VerifySignature(validated, signature, StaticTrustStore{"other": publicKey}); err == nil {
		t.Fatal("VerifySignature() accepted untrusted key")
	}

	changed := validated.Value()
	changed.Reference = "oci://registry.test/agent@sha256:" + strings.Repeat("b", 64)
	changedValidated, err := Validate(changed)
	if err != nil {
		t.Fatalf("Validate(changed) error = %v", err)
	}
	if err := VerifySignature(changedValidated, signature, StaticTrustStore{"agent-release": publicKey}); err == nil {
		t.Fatal("VerifySignature() accepted changed manifest")
	}
}

func validManifest(kernel, initrd []byte) Manifest {
	return Manifest{
		SchemaVersion:   SchemaVersion,
		MediaType:       MediaType,
		Reference:       "oci://registry.test/agent@sha256:" + strings.Repeat("a", 64),
		Architecture:    ArchitectureAMD64,
		Firmware:        FirmwareUEFI,
		PlatformProfile: PlatformProfileAMD64UEFIABV1,
		Kernel:          DescriptorFromBytes(kernel),
		Initrd:          DescriptorFromBytes(initrd),
	}
}
