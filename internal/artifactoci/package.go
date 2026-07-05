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

package artifactoci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
	oras "oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
)

const (
	ArtifactType               = "application/vnd.tart.os-artifact.v1"
	ManifestMediaType          = "application/vnd.tart.os-manifest.v1+json"
	ManifestSignatureMediaType = "application/vnd.tart.os-manifest-signature.v1+json"
	OSFilesystemMediaType      = "application/vnd.tart.os-filesystem.v1"
	VerityMediaType            = "application/vnd.tart.dm-verity.v1"
	KernelMediaType            = "application/vnd.tart.kernel.v1"
	InitrdMediaType            = "application/vnd.tart.initrd.v1"
)

type Input struct {
	Manifest   string
	Signature  string
	Image      string
	Verity     string
	Kernel     string
	Initrd     string
	SBOM       string
	Provenance string
	Reference  string
}

type layer struct {
	name      string
	mediaType string
	path      string
}

func Pack(ctx context.Context, store *file.Store, input Input) (ocispec.Descriptor, error) {
	if input.Reference == "" {
		return ocispec.Descriptor{}, errors.New("OCI reference is required")
	}
	manifest, err := verifyInput(input)
	if err != nil {
		return ocispec.Descriptor{}, err
	}
	digest, err := manifest.Digest()
	if err != nil {
		return ocispec.Descriptor{}, err
	}

	layers := []layer{
		{name: "manifest.json", mediaType: ManifestMediaType, path: input.Manifest},
		{name: "manifest.signature.json", mediaType: ManifestSignatureMediaType, path: input.Signature},
		{name: "os.img", mediaType: OSFilesystemMediaType, path: input.Image},
		{name: "os.verity", mediaType: VerityMediaType, path: input.Verity},
		{name: "vmlinuz", mediaType: KernelMediaType, path: input.Kernel},
		{name: "initrd", mediaType: InitrdMediaType, path: input.Initrd},
		{name: "sbom.cdx.json", mediaType: "application/vnd.cyclonedx+json", path: input.SBOM},
		{name: "provenance.intoto.json", mediaType: "application/vnd.in-toto+json", path: input.Provenance},
	}
	descriptors := make([]ocispec.Descriptor, 0, len(layers))
	for _, item := range layers {
		descriptor, err := store.Add(ctx, item.name, item.mediaType, item.path)
		if err != nil {
			return ocispec.Descriptor{}, fmt.Errorf("add %s: %w", item.name, err)
		}
		descriptors = append(descriptors, descriptor)
	}

	manifestDescriptor, err := oras.PackManifest(
		ctx,
		store,
		oras.PackManifestVersion1_1,
		ArtifactType,
		oras.PackManifestOptions{
			Layers: descriptors,
			ManifestAnnotations: map[string]string{
				"dev.walnuts.tart.manifest.digest": digest.String(),
			},
		},
	)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("pack OCI manifest: %w", err)
	}
	if err := store.Tag(ctx, manifestDescriptor, input.Reference); err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("tag OCI manifest: %w", err)
	}
	return manifestDescriptor, nil
}

func verifyInput(input Input) (artifact.ValidatedManifest, error) {
	manifestData, err := os.ReadFile(input.Manifest)
	if err != nil {
		return artifact.ValidatedManifest{}, fmt.Errorf("read artifact manifest: %w", err)
	}
	manifest, err := artifact.Parse(manifestData)
	if err != nil {
		return artifact.ValidatedManifest{}, err
	}

	signatureData, err := os.ReadFile(input.Signature)
	if err != nil {
		return artifact.ValidatedManifest{}, fmt.Errorf("read artifact signature: %w", err)
	}
	var signature artifact.Signature
	if err := json.Unmarshal(signatureData, &signature); err != nil {
		return artifact.ValidatedManifest{}, fmt.Errorf("decode artifact signature: %w", err)
	}
	if signature.Algorithm == "" || signature.KeyID == "" || signature.Value == "" {
		return artifact.ValidatedManifest{}, errors.New("artifact signature is incomplete")
	}

	value := manifest.Value()
	payloads := []struct {
		name   string
		path   string
		digest string
		size   int64
	}{
		{name: "image", path: input.Image, digest: value.Image.Digest, size: value.Image.SizeBytes},
		{name: "verity", path: input.Verity, digest: value.Verity.Digest, size: value.Verity.SizeBytes},
		{name: "kernel", path: input.Kernel, digest: value.Boot.KernelDigest, size: fileSize(input.Kernel)},
		{name: "initrd", path: input.Initrd, digest: value.Boot.InitrdDigest, size: fileSize(input.Initrd)},
	}
	for _, payload := range payloads {
		if payload.size <= 0 {
			return artifact.ValidatedManifest{}, fmt.Errorf("%s payload is empty or unavailable", payload.name)
		}
		if err := verifyFile(payload.path, payload.digest, payload.size); err != nil {
			return artifact.ValidatedManifest{}, fmt.Errorf("verify %s payload: %w", payload.name, err)
		}
	}
	documents := []struct {
		name string
		path string
	}{
		{name: "SBOM", path: input.SBOM},
		{name: "provenance", path: input.Provenance},
	}
	for _, document := range documents {
		if fileSize(document.path) <= 0 {
			return artifact.ValidatedManifest{}, fmt.Errorf("%s is empty or unavailable", document.name)
		}
	}
	return manifest, nil
}

func verifyFile(path, expectedDigest string, expectedSize int64) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	verifyErr := artifact.VerifyPayload(file, expectedDigest, expectedSize)
	closeErr := file.Close()
	if verifyErr != nil {
		return verifyErr
	}
	if closeErr != nil {
		return fmt.Errorf("close payload: %w", closeErr)
	}
	return nil
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return 0
	}
	return info.Size()
}
