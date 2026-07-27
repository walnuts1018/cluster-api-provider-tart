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
	"encoding/json"
	"errors"
	"fmt"
	"io"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/opencontainers/go-digest"
	"github.com/walnuts1018/cluster-api-provider-tart/artifact"
	platformprofile "github.com/walnuts1018/cluster-api-provider-tart/domain/shared/platformprofile"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/ocireference"
)

const (
	SchemaVersion                = 1
	MediaType                    = "application/vnd.tart.provisioning-agent.v1"
	ArchitectureAMD64            = "amd64"
	FirmwareUEFI                 = "UEFI"
	PlatformProfileAMD64UEFIABV1 = platformprofile.LegacyProfileAMD64UEFIABV1
	SignatureAlgorithmEd25519    = "Ed25519"
)

type Manifest struct {
	SchemaVersion   int         `json:"schemaVersion"`
	MediaType       string      `json:"mediaType"`
	Reference       string      `json:"reference"`
	Architecture    string      `json:"architecture"`
	Firmware        string      `json:"firmware"`
	PlatformProfile string      `json:"platformProfile"`
	Kernel          Descriptor  `json:"kernel"`
	Initrd          Descriptor  `json:"initrd"`
	VirtualMedia    *Descriptor `json:"virtualMedia,omitempty"`
}

type Descriptor struct {
	Digest    string `json:"digest"`
	SizeBytes int64  `json:"sizeBytes"`
}

type ValidatedManifest struct {
	manifest Manifest
}

func DescriptorFromBytes(data []byte) Descriptor {
	return Descriptor{Digest: digest.FromBytes(data).String(), SizeBytes: int64(len(data))}
}

func Parse(data []byte) (ValidatedManifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return ValidatedManifest{}, fmt.Errorf("decode Agent Artifact manifest: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return ValidatedManifest{}, errors.New("agent Artifact manifest must contain exactly one JSON value")
		}
		return ValidatedManifest{}, fmt.Errorf("decode Agent Artifact manifest trailing data: %w", err)
	}
	return Validate(manifest)
}

func Validate(manifest Manifest) (ValidatedManifest, error) {
	switch {
	case manifest.SchemaVersion != SchemaVersion:
		return ValidatedManifest{}, fmt.Errorf("unsupported Agent Artifact schemaVersion: %d", manifest.SchemaVersion)
	case manifest.MediaType != MediaType:
		return ValidatedManifest{}, fmt.Errorf("unsupported Agent Artifact mediaType: %q", manifest.MediaType)
	case !validOCIImageReference(manifest.Reference):
		return ValidatedManifest{}, errors.New("agent Artifact reference must be a valid OCI image reference")
	}
	profile, ok := platformprofile.Lookup(manifest.PlatformProfile)
	switch {
	case !ok:
		return ValidatedManifest{}, fmt.Errorf("unsupported Agent Artifact platformProfile: %q", manifest.PlatformProfile)
	case manifest.Architecture != profile.Architecture:
		return ValidatedManifest{}, fmt.Errorf("agent artifact architecture %q does not match platformProfile %q", manifest.Architecture, manifest.PlatformProfile)
	case manifest.Firmware != profile.Firmware:
		return ValidatedManifest{}, fmt.Errorf("agent artifact firmware %q does not match platformProfile %q", manifest.Firmware, manifest.PlatformProfile)
	}
	if err := validateDescriptor("kernel", manifest.Kernel); err != nil {
		return ValidatedManifest{}, err
	}
	if err := validateDescriptor("initrd", manifest.Initrd); err != nil {
		return ValidatedManifest{}, err
	}
	if manifest.VirtualMedia != nil {
		if err := validateDescriptor("virtualMedia", *manifest.VirtualMedia); err != nil {
			return ValidatedManifest{}, err
		}
	}
	return ValidatedManifest{manifest: manifest}, nil
}

func validOCIImageReference(value string) bool {
	_, err := ocireference.Parse(value)
	return err == nil
}

func validateDescriptor(name string, descriptor Descriptor) error {
	if err := digest.Digest(descriptor.Digest).Validate(); err != nil ||
		digest.Digest(descriptor.Digest).Algorithm() != digest.SHA256 {
		return fmt.Errorf("%s digest must be a canonical SHA-256 digest", name)
	}
	if descriptor.SizeBytes <= 0 {
		return fmt.Errorf("%s sizeBytes must be greater than zero", name)
	}
	return nil
}

func (manifest ValidatedManifest) Value() Manifest {
	return manifest.manifest
}

func (manifest ValidatedManifest) CanonicalJSON() ([]byte, error) {
	data, err := json.Marshal(manifest.manifest)
	if err != nil {
		return nil, fmt.Errorf("encode Agent Artifact manifest: %w", err)
	}
	canonical, err := jsoncanonicalizer.Transform(data)
	if err != nil {
		return nil, fmt.Errorf("canonicalize Agent Artifact manifest: %w", err)
	}
	return canonical, nil
}

func VerifyPayloads(manifest ValidatedManifest, kernel, initrd io.Reader) error {
	value := manifest.Value()
	if err := artifact.VerifyPayload(kernel, value.Kernel.Digest, value.Kernel.SizeBytes); err != nil {
		return fmt.Errorf("verify Agent Artifact kernel: %w", err)
	}
	if err := artifact.VerifyPayload(initrd, value.Initrd.Digest, value.Initrd.SizeBytes); err != nil {
		return fmt.Errorf("verify Agent Artifact initrd: %w", err)
	}
	return nil
}

func VerifyVirtualMediaPayload(manifest ValidatedManifest, media io.Reader) error {
	value := manifest.Value()
	if value.VirtualMedia == nil {
		return errors.New("agent Artifact does not contain a VirtualMedia payload")
	}
	if err := artifact.VerifyPayload(media, value.VirtualMedia.Digest, value.VirtualMedia.SizeBytes); err != nil {
		return fmt.Errorf("verify Agent Artifact virtual media: %w", err)
	}
	return nil
}
