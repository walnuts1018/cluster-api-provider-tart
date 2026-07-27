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

package artifactfetch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/walnuts1018/cluster-api-provider-tart/artifact"
	"github.com/walnuts1018/cluster-api-provider-tart/domain/shared/ocireference"
	agentprotocol "github.com/walnuts1018/cluster-api-provider-tart/dto/agent"
	artifactoci "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/artifact_oci"
	ociremote "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/oci_remote"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	orasretry "oras.land/oras-go/v2/registry/remote/retry"
)

const (
	maxOCIManifestBytes      = int64(1 << 20)
	maxArtifactManifestBytes = int64(1 << 20)
	maxSignatureBytes        = int64(64 << 10)
)

type repository interface {
	content.Fetcher
	FetchReference(context.Context, string) (ocispec.Descriptor, io.ReadCloser, error)
}

type repositoryFactory func(string) (repository, error)

type OCI struct {
	trustStore    artifact.TrustStore
	newRepository repositoryFactory
}

type Payload struct {
	descriptor ocispec.Descriptor
	fetcher    content.Fetcher
}

type Artifact struct {
	Manifest artifact.ValidatedManifest
	Image    Payload
	Verity   Payload
	Kernel   Payload
	Initrd   Payload
}

func NewPayload(descriptor ocispec.Descriptor, fetcher content.Fetcher) Payload {
	return Payload{descriptor: descriptor, fetcher: fetcher}
}

func NewOCI(trustStore artifact.TrustStore, credential auth.CredentialFunc) (*OCI, error) {
	if trustStore == nil {
		return nil, errors.New("artifact trust store is required")
	}
	return &OCI{
		trustStore: trustStore,
		newRepository: func(reference string) (repository, error) {
			repo, err := remote.NewRepository(reference)
			if err != nil {
				return nil, err
			}
			ociremote.AllowLoopbackPlainHTTP(repo)
			if credential != nil && !ociremote.IsLoopbackRegistry(repo.Reference.Registry) {
				repo.Client = &auth.Client{
					Client:     orasretry.DefaultClient,
					Cache:      auth.NewCache(),
					Credential: credential,
				}
			}
			return repo, nil
		},
	}, nil
}

func (payload Payload) Open(ctx context.Context) (io.ReadCloser, error) {
	reader, err := payload.fetcher.Fetch(ctx, payload.descriptor)
	if err != nil {
		return nil, fmt.Errorf("fetch payload %s: %w", payload.descriptor.Digest, err)
	}
	return reader, nil
}

// Fetchは小さいmanifestと署名だけを先に取得し、署名検証後のpayload readerを返す。
func (source *OCI) Fetch(
	ctx context.Context,
	request agentprotocol.Artifact,
	platformProfile string,
) (Artifact, error) {
	resolved, err := source.resolve(ctx, request.Ref)
	if err != nil {
		return Artifact{}, err
	}
	if err := validateIdentity(request, platformProfile, resolved.manifest, resolved.layers); err != nil {
		return Artifact{}, err
	}
	return Artifact{
		Manifest: resolved.manifest,
		Image:    Payload{descriptor: resolved.layers.image, fetcher: resolved.repository},
		Verity:   Payload{descriptor: resolved.layers.verity, fetcher: resolved.repository},
		Kernel:   Payload{descriptor: resolved.layers.kernel, fetcher: resolved.repository},
		Initrd:   Payload{descriptor: resolved.layers.initrd, fetcher: resolved.repository},
	}, nil
}

// ResolveManifestはpayload本体を取得せず、署名とOCI layer descriptorを検証したManifestを返す。
func (source *OCI) ResolveManifest(
	ctx context.Context,
	reference string,
) (artifact.ValidatedManifest, error) {
	resolved, err := source.resolve(ctx, reference)
	if err != nil {
		return artifact.ValidatedManifest{}, err
	}
	return resolved.manifest, nil
}

type resolvedArtifact struct {
	manifest   artifact.ValidatedManifest
	layers     requiredLayers
	repository repository
}

func (source *OCI) resolve(ctx context.Context, reference string) (resolvedArtifact, error) {
	ref, err := parseReference(reference)
	if err != nil {
		return resolvedArtifact{}, err
	}
	repo, err := source.newRepository(ref.Registry + "/" + ref.Repository)
	if err != nil {
		return resolvedArtifact{}, fmt.Errorf("create OCI repository client: %w", err)
	}
	descriptor, reader, err := repo.FetchReference(ctx, ref.ReferenceOrDefault())
	if err != nil {
		return resolvedArtifact{}, fmt.Errorf("fetch OCI manifest: %w", err)
	}
	if digest, err := ref.Digest(); err == nil && descriptor.Digest != digest {
		closeErr := reader.Close()
		return resolvedArtifact{}, errors.Join(errors.New("OCI manifest digest does not match reference"), closeErr)
	}
	if descriptor.MediaType != ocispec.MediaTypeImageManifest {
		closeErr := reader.Close()
		return resolvedArtifact{}, errors.Join(
			fmt.Errorf("unsupported OCI manifest media type: %q", descriptor.MediaType),
			closeErr,
		)
	}
	manifestData, err := readAndClose(reader, descriptor, maxOCIManifestBytes)
	if err != nil {
		return resolvedArtifact{}, fmt.Errorf("read OCI manifest: %w", err)
	}
	var ociManifest ocispec.Manifest
	if err := decodeStrict(manifestData, &ociManifest); err != nil {
		return resolvedArtifact{}, fmt.Errorf("decode OCI manifest: %w", err)
	}
	layers, err := selectRequiredLayers(ociManifest)
	if err != nil {
		return resolvedArtifact{}, err
	}

	artifactManifestData, err := fetchSmall(ctx, repo, layers.manifest, maxArtifactManifestBytes)
	if err != nil {
		return resolvedArtifact{}, fmt.Errorf("fetch artifact manifest: %w", err)
	}
	validatedManifest, err := artifact.Parse(artifactManifestData)
	if err != nil {
		return resolvedArtifact{}, err
	}
	signatureData, err := fetchSmall(ctx, repo, layers.signature, maxSignatureBytes)
	if err != nil {
		return resolvedArtifact{}, fmt.Errorf("fetch artifact manifest signature: %w", err)
	}
	var signature artifact.Signature
	if err := decodeStrict(signatureData, &signature); err != nil {
		return resolvedArtifact{}, fmt.Errorf("decode artifact manifest signature: %w", err)
	}
	if err := artifact.VerifySignature(validatedManifest, signature, source.trustStore); err != nil {
		return resolvedArtifact{}, fmt.Errorf("verify artifact manifest signature: %w", err)
	}
	if err := validatePayloadDescriptors(validatedManifest, layers); err != nil {
		return resolvedArtifact{}, err
	}
	return resolvedArtifact{
		manifest:   validatedManifest,
		layers:     layers,
		repository: repo,
	}, nil
}

type requiredLayers struct {
	manifest  ocispec.Descriptor
	signature ocispec.Descriptor
	image     ocispec.Descriptor
	verity    ocispec.Descriptor
	kernel    ocispec.Descriptor
	initrd    ocispec.Descriptor
}

func selectRequiredLayers(manifest ocispec.Manifest) (requiredLayers, error) {
	if manifest.MediaType != ocispec.MediaTypeImageManifest {
		return requiredLayers{}, fmt.Errorf("unsupported OCI manifest media type: %q", manifest.MediaType)
	}
	if manifest.ArtifactType != artifactoci.ArtifactType {
		return requiredLayers{}, fmt.Errorf("unsupported OCI artifact type: %q", manifest.ArtifactType)
	}
	byMediaType := make(map[string]ocispec.Descriptor, len(manifest.Layers))
	for _, layer := range manifest.Layers {
		switch layer.MediaType {
		case artifactoci.ManifestMediaType,
			artifactoci.ManifestSignatureMediaType,
			artifactoci.OSFilesystemMediaType,
			artifactoci.VerityMediaType,
			artifactoci.KernelMediaType,
			artifactoci.InitrdMediaType:
			if _, exists := byMediaType[layer.MediaType]; exists {
				return requiredLayers{}, fmt.Errorf("OCI artifact contains duplicate %q layer", layer.MediaType)
			}
			byMediaType[layer.MediaType] = layer
		}
	}
	required := []string{
		artifactoci.ManifestMediaType,
		artifactoci.ManifestSignatureMediaType,
		artifactoci.OSFilesystemMediaType,
		artifactoci.VerityMediaType,
		artifactoci.KernelMediaType,
		artifactoci.InitrdMediaType,
	}
	for _, mediaType := range required {
		if _, ok := byMediaType[mediaType]; !ok {
			return requiredLayers{}, fmt.Errorf("OCI artifact is missing %q layer", mediaType)
		}
	}
	return requiredLayers{
		manifest:  byMediaType[artifactoci.ManifestMediaType],
		signature: byMediaType[artifactoci.ManifestSignatureMediaType],
		image:     byMediaType[artifactoci.OSFilesystemMediaType],
		verity:    byMediaType[artifactoci.VerityMediaType],
		kernel:    byMediaType[artifactoci.KernelMediaType],
		initrd:    byMediaType[artifactoci.InitrdMediaType],
	}, nil
}

func validateIdentity(
	request agentprotocol.Artifact,
	platformProfile string,
	manifest artifact.ValidatedManifest,
	layers requiredLayers,
) error {
	digest, err := manifest.Digest()
	if err != nil {
		return err
	}
	value := manifest.Value()
	switch {
	case digest.String() != request.ManifestDigest:
		return errors.New("artifact manifest digest does not match Plan")
	case value.Generation != request.Generation:
		return errors.New("artifact generation does not match Plan")
	case value.PlatformProfile != platformProfile:
		return fmt.Errorf("artifact platform profile %q does not match %q", value.PlatformProfile, platformProfile)
	}
	return validatePayloadDescriptors(manifest, layers)
}

func validatePayloadDescriptors(manifest artifact.ValidatedManifest, layers requiredLayers) error {
	value := manifest.Value()
	switch {
	case layers.image.Digest.String() != value.Image.Digest || layers.image.Size != value.Image.SizeBytes:
		return errors.New("OS filesystem layer does not match artifact manifest")
	case layers.verity.Digest.String() != value.Verity.Digest || layers.verity.Size != value.Verity.SizeBytes:
		return errors.New("verity layer does not match artifact manifest")
	case layers.kernel.Digest.String() != value.Boot.KernelDigest:
		return errors.New("kernel layer does not match artifact manifest")
	case layers.initrd.Digest.String() != value.Boot.InitrdDigest:
		return errors.New("initrd layer does not match artifact manifest")
	default:
		return nil
	}
}

func parseReference(value string) (registry.Reference, error) {
	ref, err := ocireference.Parse(value)
	if err != nil {
		return registry.Reference{}, fmt.Errorf("parse OCI artifact reference: %w", err)
	}
	return ref, nil
}

func fetchSmall(
	ctx context.Context,
	fetcher content.Fetcher,
	descriptor ocispec.Descriptor,
	limit int64,
) ([]byte, error) {
	if descriptor.Size <= 0 || descriptor.Size > limit {
		return nil, fmt.Errorf("descriptor size %d exceeds allowed range", descriptor.Size)
	}
	reader, err := fetcher.Fetch(ctx, descriptor)
	if err != nil {
		return nil, err
	}
	return readAndClose(reader, descriptor, limit)
}

func readAndClose(reader io.ReadCloser, descriptor ocispec.Descriptor, limit int64) ([]byte, error) {
	if descriptor.Size <= 0 || descriptor.Size > limit {
		closeErr := reader.Close()
		return nil, errors.Join(fmt.Errorf("descriptor size %d exceeds allowed range", descriptor.Size), closeErr)
	}
	data, readErr := content.ReadAll(reader, descriptor)
	closeErr := reader.Close()
	return data, errors.Join(readErr, closeErr)
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing json.RawMessage
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("JSON must contain exactly one value")
}
