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
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/walnuts1018/cluster-api-provider-tart/artifact"
	agentprotocol "github.com/walnuts1018/cluster-api-provider-tart/dto/agent"
	artifactoci "github.com/walnuts1018/cluster-api-provider-tart/infrastructure/service/artifact_oci"
)

func TestOCIFetchVerifiesMetadataBeforeReturningPayloads(t *testing.T) {
	t.Parallel()

	source, request, repo, image, verity := newTestSource(t)
	fetched, err := source.Fetch(t.Context(), request, "amd64-uefi-ab-ubuntu-24.04-kubeadm/v1")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if fetched.Manifest.Value().Generation != request.Generation {
		t.Fatalf("Fetch() generation = %d", fetched.Manifest.Value().Generation)
	}
	assertPayload(t, fetched.Image, image)
	assertPayload(t, fetched.Verity, verity)
	assertPayload(t, fetched.Kernel, []byte("kernel"))
	assertPayload(t, fetched.Initrd, []byte("initrd"))
	if repo.fetchReferenceCount != 1 {
		t.Fatalf("FetchReference() count = %d, want 1", repo.fetchReferenceCount)
	}
}

func TestOCIFetchはタグと名前だけのOCI画像参照を解決する(t *testing.T) {
	t.Parallel()

	for _, reference := range []string{
		"oci://registry.test.walnuts.dev/tart/os",
		"oci://registry.test.walnuts.dev/tart/os:v0.1.12",
		"oci://registry.test.walnuts.dev/tart/os:v0.1.12@DIGEST",
	} {
		t.Run(reference, func(t *testing.T) {
			t.Parallel()

			source, request, repo, _, _ := newTestSource(t)
			request.Ref = strings.ReplaceAll(reference, "DIGEST", repo.reference.Digest.String())
			if _, err := source.Fetch(t.Context(), request, "amd64-uefi-ab-ubuntu-24.04-kubeadm/v1"); err != nil {
				t.Fatalf("Fetch() error = %v", err)
			}
		})
	}
}

func TestOCIResolveManifestはPayloadを取得せず署名とDescriptorを検証する(t *testing.T) {
	t.Parallel()

	source, request, repo, _, _ := newTestSource(t)
	resolved, err := source.ResolveManifest(t.Context(), request.Ref)
	if err != nil {
		t.Fatalf("ResolveManifest() error = %v", err)
	}
	if resolved.Value().Generation != request.Generation {
		t.Fatalf("ResolveManifest() generation = %d, want %d", resolved.Value().Generation, request.Generation)
	}
	if repo.payloadFetchCount != 0 {
		t.Fatalf("payload fetch count = %d, want 0", repo.payloadFetchCount)
	}
}

func TestOCIFetchRejectsPlanIdentityMismatchBeforePayloadFetch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*agentprotocol.Artifact)
	}{
		{
			name: "manifest digest",
			mutate: func(request *agentprotocol.Artifact) {
				request.ManifestDigest = digest.FromString("another manifest").String()
			},
		},
		{
			name: "generation",
			mutate: func(request *agentprotocol.Artifact) {
				request.Generation++
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			source, request, repo, _, _ := newTestSource(t)
			test.mutate(&request)
			if _, err := source.Fetch(t.Context(), request, "amd64-uefi-ab-ubuntu-24.04-kubeadm/v1"); err == nil {
				t.Fatal("Fetch() accepted mismatched Plan identity")
			}
			if repo.payloadFetchCount != 0 {
				t.Fatalf("payload fetch count = %d, want 0", repo.payloadFetchCount)
			}
		})
	}
}

func TestSelectRequiredLayersRejectsMissingAndDuplicateLayers(t *testing.T) {
	t.Parallel()

	required := []ocispec.Descriptor{
		testDescriptor([]byte("manifest"), artifactoci.ManifestMediaType),
		testDescriptor([]byte("signature"), artifactoci.ManifestSignatureMediaType),
		testDescriptor([]byte("image"), artifactoci.OSFilesystemMediaType),
		testDescriptor([]byte("verity"), artifactoci.VerityMediaType),
	}
	tests := []struct {
		name      string
		mediaType string
		layers    []ocispec.Descriptor
	}{
		{name: "missing", mediaType: ocispec.MediaTypeImageManifest, layers: required[:3]},
		{name: "duplicate", mediaType: ocispec.MediaTypeImageManifest, layers: append(required, required[2])},
		{name: "media type", mediaType: ocispec.MediaTypeImageIndex, layers: required},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if _, err := selectRequiredLayers(ocispec.Manifest{
				MediaType:    test.mediaType,
				ArtifactType: artifactoci.ArtifactType,
				Layers:       test.layers,
			}); err == nil {
				t.Fatal("selectRequiredLayers() accepted an ambiguous artifact")
			}
		})
	}
}

type fakeRepository struct {
	reference           ocispec.Descriptor
	contents            map[digest.Digest][]byte
	fetchReferenceCount int
	payloadFetchCount   int
	payloadMediaTypes   map[string]struct{}
}

func (repo *fakeRepository) FetchReference(
	_ context.Context,
	_ string,
) (ocispec.Descriptor, io.ReadCloser, error) {
	repo.fetchReferenceCount++
	return repo.reference, io.NopCloser(bytes.NewReader(repo.contents[repo.reference.Digest])), nil
}

func (repo *fakeRepository) Fetch(_ context.Context, descriptor ocispec.Descriptor) (io.ReadCloser, error) {
	if _, ok := repo.payloadMediaTypes[descriptor.MediaType]; ok {
		repo.payloadFetchCount++
	}
	return io.NopCloser(bytes.NewReader(repo.contents[descriptor.Digest])), nil
}

func newTestSource(
	t *testing.T,
) (*OCI, agentprotocol.Artifact, *fakeRepository, []byte, []byte) {
	t.Helper()

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	image := []byte("filesystem payload")
	verity := []byte("verity payload")
	imageDescriptor := testDescriptor(image, artifactoci.OSFilesystemMediaType)
	verityDescriptor := testDescriptor(verity, artifactoci.VerityMediaType)
	kernelDescriptor := testDescriptor([]byte("kernel"), artifactoci.KernelMediaType)
	initrdDescriptor := testDescriptor([]byte("initrd"), artifactoci.InitrdMediaType)
	validated, err := artifact.Validate(artifact.Manifest{
		SchemaVersion: artifact.SchemaVersion,
		MediaType:     artifact.MediaType,
		OS:            artifact.OS{Family: "ubuntu", Version: "24.04"},
		Architecture:  "amd64",
		Filesystem:    "ext4",
		Image: artifact.Payload{
			Digest:    imageDescriptor.Digest.String(),
			SizeBytes: imageDescriptor.Size,
		},
		Verity: artifact.Verity{
			Digest:    verityDescriptor.Digest.String(),
			SizeBytes: verityDescriptor.Size,
			RootHash:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
		StateSchema:     artifact.StateSchema{Min: 1, Max: 1},
		Kubernetes:      artifact.Kubernetes{Distribution: "kubeadm", LifecycleRuntime: "kubeadm.cluster.x-k8s.io/v1", Version: "v1.36.0"},
		Boot:            artifact.Boot{KernelDigest: kernelDescriptor.Digest.String(), InitrdDigest: initrdDescriptor.Digest.String()},
		Requirements:    artifact.Requirements{CPULevel: "x86-64-v1"},
		Generation:      12,
		PlatformProfile: "amd64-uefi-ab-ubuntu-24.04-kubeadm/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	manifestData, err := validated.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	signature, err := artifact.Sign(validated, "artifact-key", privateKey)
	if err != nil {
		t.Fatal(err)
	}
	signatureData, err := json.Marshal(signature)
	if err != nil {
		t.Fatal(err)
	}
	manifestDescriptor := testDescriptor(manifestData, artifactoci.ManifestMediaType)
	signatureDescriptor := testDescriptor(signatureData, artifactoci.ManifestSignatureMediaType)
	ociManifestData, err := json.Marshal(ocispec.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ocispec.MediaTypeImageManifest,
		ArtifactType: artifactoci.ArtifactType,
		Layers: []ocispec.Descriptor{
			manifestDescriptor,
			signatureDescriptor,
			imageDescriptor,
			verityDescriptor,
			kernelDescriptor,
			initrdDescriptor,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ociDescriptor := testDescriptor(ociManifestData, ocispec.MediaTypeImageManifest)
	repo := &fakeRepository{
		reference: ociDescriptor,
		contents: map[digest.Digest][]byte{
			ociDescriptor.Digest:       ociManifestData,
			manifestDescriptor.Digest:  manifestData,
			signatureDescriptor.Digest: signatureData,
			imageDescriptor.Digest:     image,
			verityDescriptor.Digest:    verity,
			kernelDescriptor.Digest:    []byte("kernel"),
			initrdDescriptor.Digest:    []byte("initrd"),
		},
		payloadMediaTypes: map[string]struct{}{
			artifactoci.OSFilesystemMediaType: {},
			artifactoci.VerityMediaType:       {},
			artifactoci.KernelMediaType:       {},
			artifactoci.InitrdMediaType:       {},
		},
	}
	source, err := NewOCI(artifact.StaticTrustStore{"artifact-key": publicKey}, nil)
	if err != nil {
		t.Fatal(err)
	}
	source.newRepository = func(string) (repository, error) {
		return repo, nil
	}
	manifestDigest, err := validated.Digest()
	if err != nil {
		t.Fatal(err)
	}
	return source, agentprotocol.Artifact{
		Ref:            "oci://registry.test.walnuts.dev/tart/os@" + ociDescriptor.Digest.String(),
		ManifestDigest: manifestDigest.String(),
		Generation:     12,
	}, repo, image, verity
}

func testDescriptor(data []byte, mediaType string) ocispec.Descriptor {
	return ocispec.Descriptor{
		MediaType: mediaType,
		Digest:    digest.FromBytes(data),
		Size:      int64(len(data)),
	}
}

func assertPayload(t *testing.T, payload Payload, expected []byte) {
	t.Helper()
	reader, err := payload.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	actual, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("payload = %q, want %q", actual, expected)
	}
}
