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

package artifact

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"

	jsoncanonicalizer "github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/opencontainers/go-digest"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/platformprofile"
)

const (
	SchemaVersion = 1
	MediaType     = "application/vnd.tart.os-slot.v1"
)

var (
	rootHashPattern        = regexp.MustCompile(`^[0-9a-f]{64}$`)
	platformProfilePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*(?:/[a-z0-9][a-z0-9._-]*)+$`)
)

type Manifest struct {
	SchemaVersion   int          `json:"schemaVersion"`
	MediaType       string       `json:"mediaType"`
	OS              OS           `json:"os"`
	Architecture    string       `json:"architecture"`
	Filesystem      string       `json:"filesystem"`
	Image           Payload      `json:"image"`
	Verity          Verity       `json:"verity"`
	StateSchema     StateSchema  `json:"stateSchema"`
	Kubernetes      Kubernetes   `json:"kubernetes"`
	Boot            Boot         `json:"boot"`
	Requirements    Requirements `json:"requirements"`
	Generation      uint64       `json:"generation"`
	PlatformProfile string       `json:"platformProfile"`
}

type OS struct {
	Family  string `json:"family"`
	Version string `json:"version"`
}

type Payload struct {
	Digest    string `json:"digest"`
	SizeBytes int64  `json:"sizeBytes"`
}

type Verity struct {
	Digest    string `json:"digest"`
	SizeBytes int64  `json:"sizeBytes"`
	RootHash  string `json:"rootHash"`
}

type StateSchema struct {
	Min uint64 `json:"min"`
	Max uint64 `json:"max"`
}

type Kubernetes struct {
	Distribution     string `json:"distribution"`
	LifecycleRuntime string `json:"lifecycleRuntime"`
	Version          string `json:"version"`
}

type Boot struct {
	KernelDigest string `json:"kernelDigest"`
	InitrdDigest string `json:"initrdDigest"`
}

type Requirements struct {
	CPULevel string `json:"cpuLevel"`
}

// ValidatedManifestは全fieldの構文と相互関係を検証済みのManifestを表す。
type ValidatedManifest struct {
	manifest Manifest
}

func Parse(data []byte) (ValidatedManifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return ValidatedManifest{}, fmt.Errorf("decode artifact manifest: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return ValidatedManifest{}, err
	}

	return Validate(manifest)
}

func Validate(manifest Manifest) (ValidatedManifest, error) {
	switch {
	case manifest.SchemaVersion != SchemaVersion:
		return ValidatedManifest{}, fmt.Errorf("unsupported schemaVersion: %d", manifest.SchemaVersion)
	case manifest.MediaType != MediaType:
		return ValidatedManifest{}, fmt.Errorf("unsupported mediaType: %q", manifest.MediaType)
	case manifest.OS.Family == "":
		return ValidatedManifest{}, errors.New("os.family is required")
	case manifest.OS.Version == "":
		return ValidatedManifest{}, errors.New("os.version is required")
	case manifest.Architecture == "":
		return ValidatedManifest{}, errors.New("architecture is required")
	case manifest.Filesystem == "":
		return ValidatedManifest{}, errors.New("filesystem is required")
	case manifest.Image.SizeBytes <= 0:
		return ValidatedManifest{}, errors.New("image.sizeBytes must be greater than zero")
	case manifest.Verity.SizeBytes <= 0:
		return ValidatedManifest{}, errors.New("verity.sizeBytes must be greater than zero")
	case !rootHashPattern.MatchString(manifest.Verity.RootHash):
		return ValidatedManifest{}, errors.New("verity.rootHash must be 64 lowercase hexadecimal characters")
	case manifest.StateSchema.Min == 0:
		return ValidatedManifest{}, errors.New("stateSchema.min must be greater than zero")
	case manifest.StateSchema.Max < manifest.StateSchema.Min:
		return ValidatedManifest{}, errors.New("stateSchema.max must be greater than or equal to stateSchema.min")
	case manifest.Kubernetes.Distribution == "":
		return ValidatedManifest{}, errors.New("kubernetes.distribution is required")
	case manifest.Kubernetes.LifecycleRuntime == "":
		return ValidatedManifest{}, errors.New("kubernetes.lifecycleRuntime is required")
	case manifest.Kubernetes.Version == "":
		return ValidatedManifest{}, errors.New("kubernetes.version is required")
	case manifest.Requirements.CPULevel == "":
		return ValidatedManifest{}, errors.New("requirements.cpuLevel is required")
	case manifest.Generation == 0:
		return ValidatedManifest{}, errors.New("generation must be greater than zero")
	case !platformProfilePattern.MatchString(manifest.PlatformProfile):
		return ValidatedManifest{}, errors.New("platformProfile must be a versioned identifier")
	}

	digests := []struct {
		name  string
		value string
	}{
		{name: "image.digest", value: manifest.Image.Digest},
		{name: "verity.digest", value: manifest.Verity.Digest},
		{name: "boot.kernelDigest", value: manifest.Boot.KernelDigest},
		{name: "boot.initrdDigest", value: manifest.Boot.InitrdDigest},
	}
	for _, item := range digests {
		if err := validateSHA256Digest(item.value); err != nil {
			return ValidatedManifest{}, fmt.Errorf("%s: %w", item.name, err)
		}
	}
	profile, ok := platformprofile.Lookup(manifest.PlatformProfile)
	if !ok {
		return ValidatedManifest{}, fmt.Errorf("unsupported platformProfile: %q", manifest.PlatformProfile)
	}
	if err := platformprofile.ValidateArtifactIdentity(profile, platformprofile.ArtifactIdentity{
		OSFamily:          manifest.OS.Family,
		OSVersion:         manifest.OS.Version,
		Architecture:      manifest.Architecture,
		Distribution:      manifest.Kubernetes.Distribution,
		LifecycleRuntime:  manifest.Kubernetes.LifecycleRuntime,
		KubernetesVersion: manifest.Kubernetes.Version,
		CPULevel:          manifest.Requirements.CPULevel,
		StateSchemaMin:    manifest.StateSchema.Min,
		StateSchemaMax:    manifest.StateSchema.Max,
	}); err != nil {
		return ValidatedManifest{}, err
	}

	return ValidatedManifest{manifest: manifest}, nil
}

func (manifest ValidatedManifest) Value() Manifest {
	return manifest.manifest
}

func (manifest ValidatedManifest) CanonicalJSON() ([]byte, error) {
	data, err := json.Marshal(manifest.manifest)
	if err != nil {
		return nil, fmt.Errorf("marshal artifact manifest: %w", err)
	}

	canonical, err := jsoncanonicalizer.Transform(data)
	if err != nil {
		return nil, fmt.Errorf("canonicalize artifact manifest: %w", err)
	}
	return canonical, nil
}

func (manifest ValidatedManifest) Digest() (digest.Digest, error) {
	canonical, err := manifest.CanonicalJSON()
	if err != nil {
		return "", err
	}
	return digest.FromBytes(canonical), nil
}

func validateSHA256Digest(value string) error {
	parsed, err := digest.Parse(value)
	if err != nil {
		return errors.New("must use sha256:<64 lowercase hexadecimal characters>")
	}
	if parsed.Algorithm() != digest.SHA256 || parsed.String() != value {
		return errors.New("must use sha256:<64 lowercase hexadecimal characters>")
	}
	return nil
}

func ensureEOF(decoder *json.Decoder) error {
	var trailing json.RawMessage
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("decode trailing artifact manifest data: %w", err)
	}
	return errors.New("artifact manifest must contain exactly one JSON value")
}
