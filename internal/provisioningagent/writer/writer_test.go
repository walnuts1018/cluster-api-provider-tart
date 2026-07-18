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
	boottrial "github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/boottrial"
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
	resolved := map[agentprotocol.DiskRole]layout.RoleDevice{}
	resolved[agentprotocol.DiskRoleOSA] = layout.RoleDevice{
		Role: agentprotocol.DiskRoleOSA, DevicePath: "os-a", SizeBytes: 8 << 30,
	}
	resolved[agentprotocol.DiskRoleVerityA] = layout.RoleDevice{
		Role: agentprotocol.DiskRoleVerityA, DevicePath: "verity-a", SizeBytes: 1 << 30,
	}
	layoutPreparer := &fakeLayout{
		resolved: resolved,
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
	if err := targetWriter.WriteTargets(t.Context(), plan, disk.Device{Path: "/dev/test", SizeBytes: 64 << 30}); err != nil {
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

func TestWriterUpdateConfiguresBootTrialMetadataWithoutChangingStateOrData(t *testing.T) {
	t.Parallel()

	image := bytes.Repeat([]byte("i"), 2<<20)
	verity := bytes.Repeat([]byte("v"), 1<<20)
	fetched := testArtifact(t, image, verity, int64(len(image)), int64(len(verity)))
	fetcher := &fakeFetcher{artifact: fetched}
	resolved := map[agentprotocol.DiskRole]layout.RoleDevice{}
	resolved[agentprotocol.DiskRoleBoot] = layout.RoleDevice{
		Role: agentprotocol.DiskRoleBoot, DevicePath: "boot", SizeBytes: 512 << 20,
	}
	resolved[agentprotocol.DiskRoleOSB] = layout.RoleDevice{
		Role: agentprotocol.DiskRoleOSB, DevicePath: "os-b", SizeBytes: 8 << 30,
	}
	resolved[agentprotocol.DiskRoleVerityB] = layout.RoleDevice{
		Role: agentprotocol.DiskRoleVerityB, DevicePath: "verity-b", SizeBytes: 1 << 30,
	}
	resolved[agentprotocol.DiskRoleState] = layout.RoleDevice{
		Role: agentprotocol.DiskRoleState, DevicePath: "state", SizeBytes: 4 << 30,
	}
	resolved[agentprotocol.DiskRoleData] = layout.RoleDevice{
		Role: agentprotocol.DiskRoleData, DevicePath: "data", SizeBytes: 32 << 30,
	}
	layoutPreparer := &fakeLayout{resolved: resolved}
	targetDirectory := t.TempDir()
	statePath := filepath.Join(targetDirectory, "state")
	dataPath := filepath.Join(targetDirectory, "data")
	stateBefore := []byte("persistent node identity")
	dataBefore := []byte("persistent volume payload")
	if err := os.WriteFile(statePath, stateBefore, 0o600); err != nil {
		t.Fatalf("write State fixture: %v", err)
	}
	if err := os.WriteFile(dataPath, dataBefore, 0o600); err != nil {
		t.Fatalf("write Data fixture: %v", err)
	}
	opener := fakeOpener{paths: map[string]string{
		"os-b":     filepath.Join(targetDirectory, "os-b"),
		"verity-b": filepath.Join(targetDirectory, "verity-b"),
		"state":    statePath,
		"data":     dataPath,
	}}
	bootTrial := &fakeBootTrialDriver{}
	targetWriter := New(layoutPreparer, fetcher, opener, nil)
	targetWriter.SetBootTrialDriver(bootTrial)
	plan := testPlan(
		t,
		agentprotocol.OperationTypeUpdate,
		"A",
		[]agentprotocol.DiskRole{
			agentprotocol.DiskRoleBoot,
			agentprotocol.DiskRoleOSB,
			agentprotocol.DiskRoleVerityB,
		},
	)

	if err := targetWriter.WriteTargets(t.Context(), plan, disk.Device{Path: "/dev/test", SizeBytes: 64 << 30}); err != nil {
		t.Fatalf("WriteTargets() error = %v", err)
	}
	if bootTrial.calls != 1 {
		t.Fatalf("Configure() calls = %d, want 1", bootTrial.calls)
	}
	if bootTrial.request.BootDevicePath != "boot" {
		t.Fatalf("BootDevicePath = %q, want boot", bootTrial.request.BootDevicePath)
	}
	if bootTrial.request.ActiveSlot != "A" || bootTrial.request.TargetSlot != "B" || bootTrial.request.RollbackSlot != "A" {
		t.Fatalf("boot trial request = %#v, want active A target B rollback A", bootTrial.request)
	}
	if bootTrial.request.ArtifactGeneration != 12 || bootTrial.request.MaxAttempts != 3 {
		t.Fatalf("boot trial request = %#v, want generation 12 attempts 3", bootTrial.request)
	}
	assertFilePrefix(t, statePath, stateBefore)
	assertFilePrefix(t, dataPath, dataBefore)
}

func TestWriterUpdateRejectsMissingBootTrialDriver(t *testing.T) {
	t.Parallel()

	image := bytes.Repeat([]byte("i"), 2<<20)
	verity := bytes.Repeat([]byte("v"), 1<<20)
	fetched := testArtifact(t, image, verity, int64(len(image)), int64(len(verity)))
	fetcher := &fakeFetcher{artifact: fetched}
	resolved := map[agentprotocol.DiskRole]layout.RoleDevice{}
	resolved[agentprotocol.DiskRoleBoot] = layout.RoleDevice{
		Role: agentprotocol.DiskRoleBoot, DevicePath: "boot", SizeBytes: 512 << 20,
	}
	resolved[agentprotocol.DiskRoleOSB] = layout.RoleDevice{
		Role: agentprotocol.DiskRoleOSB, DevicePath: "os-b", SizeBytes: 8 << 30,
	}
	resolved[agentprotocol.DiskRoleVerityB] = layout.RoleDevice{
		Role: agentprotocol.DiskRoleVerityB, DevicePath: "verity-b", SizeBytes: 1 << 30,
	}
	layoutPreparer := &fakeLayout{resolved: resolved}
	targetDirectory := t.TempDir()
	opener := fakeOpener{paths: map[string]string{
		"os-b":     filepath.Join(targetDirectory, "os-b"),
		"verity-b": filepath.Join(targetDirectory, "verity-b"),
	}}
	targetWriter := New(layoutPreparer, fetcher, opener, nil)
	plan := testPlan(
		t,
		agentprotocol.OperationTypeUpdate,
		"A",
		[]agentprotocol.DiskRole{
			agentprotocol.DiskRoleBoot,
			agentprotocol.DiskRoleOSB,
			agentprotocol.DiskRoleVerityB,
		},
	)

	if err := targetWriter.WriteTargets(t.Context(), plan, disk.Device{Path: "/dev/test", SizeBytes: 64 << 30}); err == nil {
		t.Fatal("WriteTargets() accepted an Update Plan without a boot trial driver")
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
	if err := targetWriter.WriteTargets(t.Context(), plan, disk.Device{}); err == nil {
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
	if err := targetWriter.WriteTargets(t.Context(), plan, disk.Device{}); err == nil {
		t.Fatal("WriteTargets() accepted an oversized OS payload")
	}
	if layoutPreparer.calls != 0 {
		t.Fatalf("Prepare() calls = %d, want 0", layoutPreparer.calls)
	}
}

func TestWriterWipeAllZeroesWholeDiskWithoutArtifactFetch(t *testing.T) {
	t.Parallel()

	targetDirectory := t.TempDir()
	rootPath := filepath.Join(targetDirectory, "root-disk")
	if err := os.WriteFile(rootPath, bytes.Repeat([]byte("x"), 2<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	fetcher := &fakeFetcher{}
	layoutPreparer := &fakeLayout{}
	opener := fakeOpener{paths: map[string]string{
		"/dev/test": rootPath,
	}}
	targetWriter := New(layoutPreparer, fetcher, opener, nil)
	plan := cleaningPlan(t, agentprotocol.OperationTypeWipeAll, []agentprotocol.DiskRole{
		agentprotocol.DiskRoleBoot,
		agentprotocol.DiskRoleOSA,
		agentprotocol.DiskRoleOSB,
		agentprotocol.DiskRoleVerityA,
		agentprotocol.DiskRoleVerityB,
		agentprotocol.DiskRoleState,
		agentprotocol.DiskRoleData,
	})
	if err := targetWriter.WriteTargets(t.Context(), plan, disk.Device{Path: "/dev/test", SizeBytes: 2 << 20}); err != nil {
		t.Fatalf("WriteTargets() error = %v", err)
	}
	assertZeroContent(t, rootPath, 2<<20)
	if layoutPreparer.calls != 0 || fetcher.calls != 0 {
		t.Fatalf("calls: layout=%d fetch=%d", layoutPreparer.calls, fetcher.calls)
	}
}

func TestWriterWipeAllPrefersSanitizeWhenSupported(t *testing.T) {
	t.Parallel()

	targetDirectory := t.TempDir()
	rootPath := filepath.Join(targetDirectory, "root-disk")
	original := bytes.Repeat([]byte("x"), 2<<20)
	if err := os.WriteFile(rootPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	fetcher := &fakeFetcher{}
	layoutPreparer := &fakeLayout{}
	opener := fakeOpener{paths: map[string]string{
		"/dev/test": rootPath,
	}}
	sanitizer := &fakeSanitizer{supported: true}
	targetWriter := NewWithSanitizer(layoutPreparer, fetcher, opener, sanitizer, nil)
	plan := cleaningPlan(t, agentprotocol.OperationTypeWipeAll, []agentprotocol.DiskRole{
		agentprotocol.DiskRoleBoot,
		agentprotocol.DiskRoleOSA,
		agentprotocol.DiskRoleOSB,
		agentprotocol.DiskRoleVerityA,
		agentprotocol.DiskRoleVerityB,
		agentprotocol.DiskRoleState,
		agentprotocol.DiskRoleData,
	})
	if err := targetWriter.WriteTargets(t.Context(), plan, disk.Device{Path: "/dev/test", SizeBytes: 2 << 20}); err != nil {
		t.Fatalf("WriteTargets() error = %v", err)
	}
	assertFilePrefix(t, rootPath, original)
	if sanitizer.calls != 1 || sanitizer.path != "/dev/test" || sanitizer.sizeBytes != 2<<20 {
		t.Fatalf("sanitizer = %#v", sanitizer)
	}
	if layoutPreparer.calls != 0 || fetcher.calls != 0 {
		t.Fatalf("calls: layout=%d fetch=%d", layoutPreparer.calls, fetcher.calls)
	}
}

func TestWriterWipeAllFallsBackToZeroOverwriteWhenSanitizeIsUnavailable(t *testing.T) {
	t.Parallel()

	targetDirectory := t.TempDir()
	rootPath := filepath.Join(targetDirectory, "root-disk")
	if err := os.WriteFile(rootPath, bytes.Repeat([]byte("x"), 2<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	fetcher := &fakeFetcher{}
	layoutPreparer := &fakeLayout{}
	opener := fakeOpener{paths: map[string]string{
		"/dev/test": rootPath,
	}}
	sanitizer := &fakeSanitizer{}
	targetWriter := NewWithSanitizer(layoutPreparer, fetcher, opener, sanitizer, nil)
	plan := cleaningPlan(t, agentprotocol.OperationTypeWipeAll, []agentprotocol.DiskRole{
		agentprotocol.DiskRoleBoot,
		agentprotocol.DiskRoleOSA,
		agentprotocol.DiskRoleOSB,
		agentprotocol.DiskRoleVerityA,
		agentprotocol.DiskRoleVerityB,
		agentprotocol.DiskRoleState,
		agentprotocol.DiskRoleData,
	})
	if err := targetWriter.WriteTargets(t.Context(), plan, disk.Device{Path: "/dev/test", SizeBytes: 2 << 20}); err != nil {
		t.Fatalf("WriteTargets() error = %v", err)
	}
	assertZeroContent(t, rootPath, 2<<20)
	if sanitizer.calls != 1 {
		t.Fatalf("Sanitize() calls = %d, want 1", sanitizer.calls)
	}
	if layoutPreparer.calls != 0 || fetcher.calls != 0 {
		t.Fatalf("calls: layout=%d fetch=%d", layoutPreparer.calls, fetcher.calls)
	}
}

func TestWriterCleanZeroesOnlyAllowedRoles(t *testing.T) {
	t.Parallel()

	targetDirectory := t.TempDir()
	osPath := filepath.Join(targetDirectory, "os-a")
	statePath := filepath.Join(targetDirectory, "state")
	dataPath := filepath.Join(targetDirectory, "data")
	for _, file := range []string{osPath, statePath, dataPath} {
		if err := os.WriteFile(file, bytes.Repeat([]byte("x"), 1<<20), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fetcher := &fakeFetcher{}
	resolved := map[agentprotocol.DiskRole]layout.RoleDevice{}
	resolved[agentprotocol.DiskRoleOSA] = layout.RoleDevice{
		Role: agentprotocol.DiskRoleOSA, DevicePath: "os-a", SizeBytes: 1 << 20,
	}
	resolved[agentprotocol.DiskRoleState] = layout.RoleDevice{
		Role: agentprotocol.DiskRoleState, DevicePath: "state", SizeBytes: 1 << 20,
	}
	resolved[agentprotocol.DiskRoleData] = layout.RoleDevice{
		Role: agentprotocol.DiskRoleData, DevicePath: "data", SizeBytes: 1 << 20,
	}
	layoutPreparer := &fakeLayout{
		resolved: resolved,
	}
	opener := fakeOpener{paths: map[string]string{
		"os-a":  osPath,
		"state": statePath,
		"data":  dataPath,
	}}
	targetWriter := New(layoutPreparer, fetcher, opener, nil)
	plan := cleaningPlan(t, agentprotocol.OperationTypeClean, []agentprotocol.DiskRole{
		agentprotocol.DiskRoleOSA,
		agentprotocol.DiskRoleState,
	})
	if err := targetWriter.WriteTargets(t.Context(), plan, disk.Device{Path: "/dev/test", SizeBytes: 4 << 20}); err != nil {
		t.Fatalf("WriteTargets() error = %v", err)
	}
	assertZeroContent(t, osPath, 1<<20)
	assertZeroContent(t, statePath, 1<<20)
	assertFilePrefix(t, dataPath, bytes.Repeat([]byte("x"), 1<<20))
	if layoutPreparer.calls != 1 || fetcher.calls != 0 {
		t.Fatalf("calls: layout=%d fetch=%d", layoutPreparer.calls, fetcher.calls)
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

type fakeSanitizer struct {
	supported bool
	err       error
	calls     int
	path      string
	sizeBytes int64
}

type fakeBootTrialDriver struct {
	request boottrial.Request
	calls   int
	err     error
}

func (driver *fakeBootTrialDriver) Configure(_ context.Context, request boottrial.Request) error {
	driver.calls++
	driver.request = request
	return driver.err
}

func (sanitizer *fakeSanitizer) Sanitize(
	_ context.Context,
	devicePath string,
	sizeBytes int64,
) (bool, error) {
	sanitizer.calls++
	sanitizer.path = devicePath
	sanitizer.sizeBytes = sizeBytes
	return sanitizer.supported, sanitizer.err
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
		Kubernetes:      artifact.Kubernetes{Distribution: "kubeadm", LifecycleRuntime: "kubeadm.cluster.x-k8s.io/v1", Version: "v1.36.0"},
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
		Artifact: &agentprotocol.Artifact{
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

func cleaningPlan(
	t *testing.T,
	operation agentprotocol.OperationType,
	roles []agentprotocol.DiskRole,
) agentprotocol.ValidatedPlan {
	t.Helper()
	plan, err := agentprotocol.ValidatePlan(agentprotocol.Plan{
		APIVersion:    agentprotocol.APIVersion,
		OperationUID:  "operation-uid",
		HostUID:       "host-uid",
		OperationType: operation,
		Deadline:      time.Now().Add(time.Hour),
		RootDevice: agentprotocol.RootDevice{
			DeviceName:   "/dev/disk/by-id/test",
			SerialNumber: "serial",
			MinSizeBytes: 64 << 30,
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

func assertZeroContent(t *testing.T, path string, size int) {
	t.Helper()
	assertFilePrefix(t, path, make([]byte, size))
}
