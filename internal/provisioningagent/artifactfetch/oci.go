package artifactfetch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/artifactoci"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
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
			if credential != nil {
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
	ref, err := parseReference(request.Ref)
	if err != nil {
		return Artifact{}, err
	}
	repo, err := source.newRepository(ref.Registry + "/" + ref.Repository)
	if err != nil {
		return Artifact{}, fmt.Errorf("create OCI repository client: %w", err)
	}
	descriptor, reader, err := repo.FetchReference(ctx, ref.Reference)
	if err != nil {
		return Artifact{}, fmt.Errorf("fetch OCI manifest: %w", err)
	}
	if descriptor.Digest.String() != ref.Reference {
		closeErr := reader.Close()
		return Artifact{}, errors.Join(errors.New("OCI manifest digest does not match pinned reference"), closeErr)
	}
	manifestData, err := readAndClose(reader, descriptor, maxOCIManifestBytes)
	if err != nil {
		return Artifact{}, fmt.Errorf("read OCI manifest: %w", err)
	}
	var ociManifest ocispec.Manifest
	if err := decodeStrict(manifestData, &ociManifest); err != nil {
		return Artifact{}, fmt.Errorf("decode OCI manifest: %w", err)
	}
	layers, err := selectRequiredLayers(ociManifest)
	if err != nil {
		return Artifact{}, err
	}

	artifactManifestData, err := fetchSmall(ctx, repo, layers.manifest, maxArtifactManifestBytes)
	if err != nil {
		return Artifact{}, fmt.Errorf("fetch artifact manifest: %w", err)
	}
	validatedManifest, err := artifact.Parse(artifactManifestData)
	if err != nil {
		return Artifact{}, err
	}
	signatureData, err := fetchSmall(ctx, repo, layers.signature, maxSignatureBytes)
	if err != nil {
		return Artifact{}, fmt.Errorf("fetch artifact manifest signature: %w", err)
	}
	var signature artifact.Signature
	if err := decodeStrict(signatureData, &signature); err != nil {
		return Artifact{}, fmt.Errorf("decode artifact manifest signature: %w", err)
	}
	if err := artifact.VerifySignature(validatedManifest, signature, source.trustStore); err != nil {
		return Artifact{}, fmt.Errorf("verify artifact manifest signature: %w", err)
	}
	if err := validateIdentity(request, platformProfile, validatedManifest, layers); err != nil {
		return Artifact{}, err
	}
	return Artifact{
		Manifest: validatedManifest,
		Image:    Payload{descriptor: layers.image, fetcher: repo},
		Verity:   Payload{descriptor: layers.verity, fetcher: repo},
	}, nil
}

type requiredLayers struct {
	manifest  ocispec.Descriptor
	signature ocispec.Descriptor
	image     ocispec.Descriptor
	verity    ocispec.Descriptor
}

func selectRequiredLayers(manifest ocispec.Manifest) (requiredLayers, error) {
	if manifest.ArtifactType != artifactoci.ArtifactType {
		return requiredLayers{}, fmt.Errorf("unsupported OCI artifact type: %q", manifest.ArtifactType)
	}
	byMediaType := make(map[string]ocispec.Descriptor, len(manifest.Layers))
	for _, layer := range manifest.Layers {
		switch layer.MediaType {
		case artifactoci.ManifestMediaType,
			artifactoci.ManifestSignatureMediaType,
			artifactoci.OSFilesystemMediaType,
			artifactoci.VerityMediaType:
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
	case layers.image.Digest.String() != value.Image.Digest || layers.image.Size != value.Image.SizeBytes:
		return errors.New("OS filesystem layer does not match artifact manifest")
	case layers.verity.Digest.String() != value.Verity.Digest || layers.verity.Size != value.Verity.SizeBytes:
		return errors.New("Verity layer does not match artifact manifest")
	}
	return nil
}

func parseReference(value string) (registry.Reference, error) {
	if !strings.HasPrefix(value, "oci://") {
		return registry.Reference{}, errors.New("artifact reference must use oci://")
	}
	ref, err := registry.ParseReference(strings.TrimPrefix(value, "oci://"))
	if err != nil {
		return registry.Reference{}, fmt.Errorf("parse OCI artifact reference: %w", err)
	}
	if err := ref.ValidateReferenceAsDigest(); err != nil {
		return registry.Reference{}, fmt.Errorf("validate OCI artifact digest reference: %w", err)
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
