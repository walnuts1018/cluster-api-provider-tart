package artifactoci

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
	"oras.land/oras-go/v2/content/file"
)

func TestPackIsDeterministic(t *testing.T) {
	t.Parallel()

	input := createInput(t)
	first := packInNewStore(t, input)
	second := packInNewStore(t, input)
	if first.Digest != second.Digest {
		t.Fatalf("Pack() digest differs: first %s, second %s", first.Digest, second.Digest)
	}
	if first.Size != second.Size {
		t.Fatalf("Pack() size differs: first %d, second %d", first.Size, second.Size)
	}
}

func TestPackCreatesExpectedArtifact(t *testing.T) {
	t.Parallel()

	input := createInput(t)
	store, err := file.New(t.TempDir())
	if err != nil {
		t.Fatalf("file.New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("store.Close() error = %v", err)
		}
	})

	descriptor, err := Pack(t.Context(), store, input)
	if err != nil {
		t.Fatalf("Pack() error = %v", err)
	}
	reader, err := store.Fetch(t.Context(), descriptor)
	if err != nil {
		t.Fatalf("store.Fetch() error = %v", err)
	}
	var manifest ocispec.Manifest
	decodeErr := json.NewDecoder(reader).Decode(&manifest)
	closeErr := reader.Close()
	if decodeErr != nil {
		t.Fatalf("json.Decode() error = %v", decodeErr)
	}
	if closeErr != nil {
		t.Fatalf("reader.Close() error = %v", closeErr)
	}
	if manifest.ArtifactType != ArtifactType {
		t.Fatalf("ArtifactType = %q, want %q", manifest.ArtifactType, ArtifactType)
	}
	if len(manifest.Layers) != 8 {
		t.Fatalf("len(Layers) = %d, want 8", len(manifest.Layers))
	}
}

func TestPackRejectsChangedPayload(t *testing.T) {
	t.Parallel()

	input := createInput(t)
	if err := os.WriteFile(input.Image, []byte("changed payload"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	store, err := file.New(t.TempDir())
	if err != nil {
		t.Fatalf("file.New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("store.Close() error = %v", err)
		}
	})

	if _, err := Pack(t.Context(), store, input); err == nil {
		t.Fatal("Pack() error = nil, want payload verification error")
	}
}

func packInNewStore(t *testing.T, input Input) ocispec.Descriptor {
	t.Helper()

	store, err := file.New(t.TempDir())
	if err != nil {
		t.Fatalf("file.New() error = %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Errorf("store.Close() error = %v", err)
		}
	}()

	descriptor, err := Pack(t.Context(), store, input)
	if err != nil {
		t.Fatalf("Pack() error = %v", err)
	}
	return descriptor
}

func createInput(t *testing.T) Input {
	t.Helper()

	root := t.TempDir()
	image := writeFile(t, root, "os.img", []byte("filesystem image"))
	verity := writeFile(t, root, "os.verity", []byte("verity payload"))
	kernel := writeFile(t, root, "vmlinuz", []byte("kernel payload"))
	initrd := writeFile(t, root, "initrd", []byte("initrd payload"))

	imageDescription := describe(t, image)
	verityDescription := describe(t, verity)
	kernelDescription := describe(t, kernel)
	initrdDescription := describe(t, initrd)
	manifest, err := artifact.Validate(artifact.Manifest{
		SchemaVersion: artifact.SchemaVersion,
		MediaType:     artifact.MediaType,
		OS:            artifact.OS{Family: "ubuntu", Version: "24.04"},
		Architecture:  "amd64",
		Filesystem:    "ext4",
		Image:         imageDescription,
		Verity: artifact.Verity{
			Digest:    verityDescription.Digest,
			SizeBytes: verityDescription.SizeBytes,
			RootHash:  strings.Repeat("a", 64),
		},
		StateSchema:     artifact.StateSchema{Min: 1, Max: 1},
		Kubernetes:      artifact.Kubernetes{Distribution: "kubeadm", Version: "v1.35.0"},
		Boot:            artifact.Boot{KernelDigest: kernelDescription.Digest, InitrdDigest: initrdDescription.Digest},
		Requirements:    artifact.Requirements{CPULevel: "x86-64-v1"},
		Generation:      1,
		PlatformProfile: "amd64-uefi-ab/v1",
	})
	if err != nil {
		t.Fatalf("artifact.Validate() error = %v", err)
	}
	manifestJSON, err := manifest.CanonicalJSON()
	if err != nil {
		t.Fatalf("manifest.CanonicalJSON() error = %v", err)
	}
	manifestPath := writeFile(t, root, "manifest.json", manifestJSON)

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey() error = %v", err)
	}
	signature, err := artifact.Sign(manifest, "test-key", privateKey)
	if err != nil {
		t.Fatalf("artifact.Sign() error = %v", err)
	}
	signatureJSON, err := json.Marshal(signature)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	return Input{
		Manifest:   manifestPath,
		Signature:  writeFile(t, root, "manifest.signature.json", signatureJSON),
		Image:      image,
		Verity:     verity,
		Kernel:     kernel,
		Initrd:     initrd,
		SBOM:       writeFile(t, root, "sbom.cdx.json", []byte(`{"bomFormat":"CycloneDX"}`)),
		Provenance: writeFile(t, root, "provenance.intoto.json", []byte(`{"_type":"https://in-toto.io/Statement/v1"}`)),
		Reference:  "test",
	}
}

func describe(t *testing.T, path string) artifact.Payload {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("os.Open() error = %v", err)
	}
	description, describeErr := artifact.DescribePayload(file)
	closeErr := file.Close()
	if describeErr != nil {
		t.Fatalf("artifact.DescribePayload() error = %v", describeErr)
	}
	if closeErr != nil {
		t.Fatalf("file.Close() error = %v", closeErr)
	}
	return description
}

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	return path
}
