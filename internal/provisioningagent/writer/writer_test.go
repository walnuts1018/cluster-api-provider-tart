package writer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/artifactfetch"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/disk"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/layout"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/artifact"
)

func TestSelectPayloadTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		operation agentprotocol.OperationType
		active    string
		os        agentprotocol.DiskRole
		verity    agentprotocol.DiskRole
	}{
		{
			name:      "ProvisionはA",
			operation: agentprotocol.OperationTypeProvision,
			os:        agentprotocol.DiskRoleOSA,
			verity:    agentprotocol.DiskRoleVerityA,
		},
		{
			name:      "A稼働中のUpdateはB",
			operation: agentprotocol.OperationTypeUpdate,
			active:    "A",
			os:        agentprotocol.DiskRoleOSB,
			verity:    agentprotocol.DiskRoleVerityB,
		},
		{
			name:      "B稼働中のUpdateはA",
			operation: agentprotocol.OperationTypeUpdate,
			active:    "B",
			os:        agentprotocol.DiskRoleOSA,
			verity:    agentprotocol.DiskRoleVerityA,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := testPlan(t, test.operation, test.active, []agentprotocol.DiskRole{test.os, test.verity})
			actual, err := selectPayloadTargets(plan)
			if err != nil {
				t.Fatalf("selectPayloadTargets() error = %v", err)
			}
			if actual.os != test.os || actual.verity != test.verity {
				t.Fatalf("selectPayloadTargets() = %#v", actual)
			}
		})
	}
}

func TestWriterWritesAndReadsBackOSAndVerity(t *testing.T) {
	t.Parallel()

	image := bytes.Repeat([]byte("i"), 2<<20)
	verity := bytes.Repeat([]byte("v"), 1<<20)
	fetched := testArtifact(t, image, verity, int64(len(image)), int64(len(verity)))
	fetcher := &fakeFetcher{artifact: fetched}
	layoutPreparer := &fakeLayout{
		resolved: map[agentprotocol.DiskRole]layout.RoleDevice{
			agentprotocol.DiskRoleOSA:     {Role: agentprotocol.DiskRoleOSA, DevicePath: "os-a", SizeBytes: 8 << 30},
			agentprotocol.DiskRoleVerityA: {Role: agentprotocol.DiskRoleVerityA, DevicePath: "verity-a", SizeBytes: 1 << 30},
		},
	}
	targetDirectory := t.TempDir()
	opener := fakeOpener{paths: map[string]string{
		"os-a":     filepath.Join(targetDirectory, "os-a"),
		"verity-a": filepath.Join(targetDirectory, "verity-a"),
	}}
	var progress []Progress
	targetWriter := New(
		layoutPreparer,
		fetcher,
		opener,
		func(_ context.Context, event Progress) error {
			progress = append(progress, event)
			return nil
		},
	)
	plan := testPlan(
		t,
		agentprotocol.OperationTypeProvision,
		"",
		[]agentprotocol.DiskRole{agentprotocol.DiskRoleOSA, agentprotocol.DiskRoleVerityA},
	)
	if err := targetWriter.WriteTargets(context.Background(), plan, disk.Device{Path: "/dev/test", SizeBytes: 64 << 30}); err != nil {
		t.Fatalf("WriteTargets() error = %v", err)
	}
	assertFilePrefix(t, opener.paths["os-a"], image)
	assertFilePrefix(t, opener.paths["verity-a"], verity)
	if layoutPreparer.calls != 1 || fetcher.calls != 1 {
		t.Fatalf("calls: layout=%d fetch=%d", layoutPreparer.calls, fetcher.calls)
	}
	if len(progress) != 22 ||
		progress[0] != (Progress{
			Step: agentprotocol.StepWriteImage, DiskRole: agentprotocol.DiskRoleOSA, Percent: 10,
		}) ||
		progress[19] != (Progress{
			Step: agentprotocol.StepWriteImage, DiskRole: agentprotocol.DiskRoleVerityA, Percent: 100,
		}) ||
		progress[20] != (Progress{Step: agentprotocol.StepWriteImage, Percent: 100, Completed: true}) ||
		progress[21] != (Progress{Step: agentprotocol.StepVerifyImage, Percent: 100, Completed: true}) {
		t.Fatalf("progress = %#v", progress)
	}
}

func TestWriterVerifiesArtifactBeforeDestructiveLayout(t *testing.T) {
	t.Parallel()

	layoutPreparer := &fakeLayout{}
	targetWriter := New(layoutPreparer, &fakeFetcher{err: errors.New("untrusted")}, fakeOpener{}, nil)
	plan := testPlan(
		t,
		agentprotocol.OperationTypeProvision,
		"",
		[]agentprotocol.DiskRole{agentprotocol.DiskRoleOSA, agentprotocol.DiskRoleVerityA},
	)
	if err := targetWriter.WriteTargets(context.Background(), plan, disk.Device{}); err == nil {
		t.Fatal("WriteTargets() accepted an untrusted Artifact")
	}
	if layoutPreparer.calls != 0 {
		t.Fatalf("Prepare() calls = %d, want 0", layoutPreparer.calls)
	}
}

func TestWriterRejectsOversizedPayloadBeforeDestructiveLayout(t *testing.T) {
	t.Parallel()

	layoutPreparer := &fakeLayout{}
	fetched := testArtifact(t, nil, nil, (8<<30)+1, 1<<20)
	targetWriter := New(layoutPreparer, &fakeFetcher{artifact: fetched}, fakeOpener{}, nil)
	plan := testPlan(
		t,
		agentprotocol.OperationTypeProvision,
		"",
		[]agentprotocol.DiskRole{agentprotocol.DiskRoleOSA, agentprotocol.DiskRoleVerityA},
	)
	if err := targetWriter.WriteTargets(context.Background(), plan, disk.Device{}); err == nil {
		t.Fatal("WriteTargets() accepted an oversized OS payload")
	}
	if layoutPreparer.calls != 0 {
		t.Fatalf("Prepare() calls = %d, want 0", layoutPreparer.calls)
	}
}

type fakeFetcher struct {
	artifact artifactfetch.Artifact
	err      error
	calls    int
}

func (fetcher *fakeFetcher) Fetch(
	context.Context,
	agentprotocol.Artifact,
	string,
) (artifactfetch.Artifact, error) {
	fetcher.calls++
	return fetcher.artifact, fetcher.err
}

type fakeLayout struct {
	resolved map[agentprotocol.DiskRole]layout.RoleDevice
	err      error
	calls    int
}

func (preparer *fakeLayout) Prepare(
	context.Context,
	agentprotocol.OperationType,
	disk.Device,
) (map[agentprotocol.DiskRole]layout.RoleDevice, error) {
	preparer.calls++
	return preparer.resolved, preparer.err
}

type fakeOpener struct {
	paths map[string]string
}

func (opener fakeOpener) Open(path string) (SyncReadWriteSeekCloser, error) {
	targetPath, ok := opener.paths[path]
	if !ok {
		return nil, fmt.Errorf("unexpected device path %q", path)
	}
	return os.OpenFile(targetPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
}

type memoryFetcher map[digest.Digest][]byte

func (fetcher memoryFetcher) Fetch(
	_ context.Context,
	descriptor ocispec.Descriptor,
) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(fetcher[descriptor.Digest])), nil
}

func testArtifact(
	t *testing.T,
	image, verity []byte,
	imageSize, veritySize int64,
) artifactfetch.Artifact {
	t.Helper()

	imageDescriptor := descriptorFor(image, imageSize)
	verityDescriptor := descriptorFor(verity, veritySize)
	manifest, err := artifact.Validate(artifact.Manifest{
		SchemaVersion: artifact.SchemaVersion,
		MediaType:     artifact.MediaType,
		OS:            artifact.OS{Family: "ubuntu", Version: "24.04"},
		Architecture:  "amd64",
		Filesystem:    "ext4",
		Image: artifact.Payload{
			Digest:    imageDescriptor.Digest.String(),
			SizeBytes: imageSize,
		},
		Verity: artifact.Verity{
			Digest:    verityDescriptor.Digest.String(),
			SizeBytes: veritySize,
			RootHash:  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
		StateSchema:     artifact.StateSchema{Min: 1, Max: 1},
		Kubernetes:      artifact.Kubernetes{Distribution: "kubeadm", Version: "v1.35.0"},
		Boot:            artifact.Boot{KernelDigest: digest.FromString("kernel").String(), InitrdDigest: digest.FromString("initrd").String()},
		Requirements:    artifact.Requirements{CPULevel: "x86-64-v1"},
		Generation:      12,
		PlatformProfile: layout.ProfileID,
	})
	if err != nil {
		t.Fatal(err)
	}
	fetcher := memoryFetcher{
		imageDescriptor.Digest:  image,
		verityDescriptor.Digest: verity,
	}
	return artifactfetch.Artifact{
		Manifest: manifest,
		Image:    artifactfetch.NewPayload(imageDescriptor, fetcher),
		Verity:   artifactfetch.NewPayload(verityDescriptor, fetcher),
	}
}

func descriptorFor(data []byte, size int64) ocispec.Descriptor {
	payloadDigest := digest.FromBytes(data)
	if len(data) == 0 {
		payloadDigest = digest.FromString("placeholder")
	}
	return ocispec.Descriptor{Digest: payloadDigest, Size: size}
}

func testPlan(
	t *testing.T,
	operation agentprotocol.OperationType,
	active string,
	roles []agentprotocol.DiskRole,
) agentprotocol.ValidatedPlan {
	t.Helper()
	plan, err := agentprotocol.ValidatePlan(agentprotocol.Plan{
		APIVersion:    agentprotocol.APIVersion,
		OperationUID:  "operation-uid",
		HostUID:       "host-uid",
		OperationType: operation,
		ActiveSlot:    active,
		Deadline:      time.Now().Add(time.Hour),
		RootDevice: agentprotocol.RootDevice{
			DeviceName:   "/dev/disk/by-id/test",
			SerialNumber: "serial",
			MinSizeBytes: 64 << 30,
		},
		Artifact: agentprotocol.Artifact{
			Ref:            "oci://registry.test.walnuts.dev/tart/os@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			ManifestDigest: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Generation:     12,
		},
		AllowedTargetRoles: roles,
		Steps: []agentprotocol.PlanStep{
			{Name: agentprotocol.StepWriteImage},
			{Name: agentprotocol.StepVerifyImage},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func assertFilePrefix(t *testing.T, path string, expected []byte) {
	t.Helper()
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("%s content does not match payload", path)
	}
}
