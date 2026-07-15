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
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/opencontainers/go-digest"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/artifactfetch"
	boottrial "github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/boottrial"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/disk"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/layout"
	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/payload"
	"github.com/walnuts1018/cluster-api-provider-tart/pkg/agentprotocol"
)

type LayoutPreparer interface {
	Prepare(context.Context, agentprotocol.OperationType, disk.Device) (map[agentprotocol.DiskRole]layout.RoleDevice, error)
}

type ArtifactFetcher interface {
	Fetch(context.Context, agentprotocol.Artifact, string) (artifactfetch.Artifact, error)
}

type SyncReadWriteSeekCloser interface {
	payload.SyncReadWriteSeeker
	io.Closer
}

type DeviceOpener interface {
	Open(string) (SyncReadWriteSeekCloser, error)
}

type Sanitizer interface {
	Sanitize(context.Context, string, int64) (bool, error)
}

type Progress struct {
	Step      string
	DiskRole  agentprotocol.DiskRole
	Percent   int32
	Completed bool
}

type ProgressReporter func(context.Context, Progress) error

type Writer struct {
	layout    LayoutPreparer
	fetcher   ArtifactFetcher
	opener    DeviceOpener
	sanitize  Sanitizer
	progress  ProgressReporter
	bootTrial boottrial.Driver
}

func New(
	layoutPreparer LayoutPreparer,
	fetcher ArtifactFetcher,
	opener DeviceOpener,
	progress ProgressReporter,
) *Writer {
	return NewWithSanitizer(layoutPreparer, fetcher, opener, nil, progress)
}

func NewWithSanitizer(
	layoutPreparer LayoutPreparer,
	fetcher ArtifactFetcher,
	opener DeviceOpener,
	sanitizer Sanitizer,
	progress ProgressReporter,
) *Writer {
	return &Writer{
		layout:   layoutPreparer,
		fetcher:  fetcher,
		opener:   opener,
		sanitize: sanitizer,
		progress: progress,
	}
}

func (writer *Writer) SetBootTrialDriver(driver boottrial.Driver) {
	writer.bootTrial = driver
}

func (writer *Writer) WriteTargets(
	ctx context.Context,
	plan agentprotocol.ValidatedPlan,
	device disk.Device,
) error {
	switch plan.Value().OperationType {
	case agentprotocol.OperationTypeProvision, agentprotocol.OperationTypeUpdate:
		// OS Artifactの書き込みはこの後の共通処理で行う。
	case agentprotocol.OperationTypeClean, agentprotocol.OperationTypeWipeAll:
		return writer.cleanTargets(ctx, plan, device)
	default:
		return fmt.Errorf("unsupported operation type %q", plan.Value().OperationType)
	}
	if err := validateProgressSteps(plan.Value()); err != nil {
		return err
	}
	targetRoles, err := selectPayloadTargets(plan)
	if err != nil {
		return err
	}
	if plan.Value().Artifact == nil {
		return errors.New("plan artifact is required")
	}
	fetched, err := writer.fetcher.Fetch(ctx, *plan.Value().Artifact, layout.ProfileID)
	if err != nil {
		return fmt.Errorf("fetch and verify OS artifact: %w", err)
	}
	manifest := fetched.Manifest.Value()
	if err := validateProfileCapacity(targetRoles, manifest.Image.SizeBytes, manifest.Verity.SizeBytes); err != nil {
		return err
	}
	resolved, err := writer.layout.Prepare(ctx, plan.Value().OperationType, device)
	if err != nil {
		return fmt.Errorf("prepare partition layout: %w", err)
	}
	if err := validateCapacity(resolved, targetRoles, manifest.Image.SizeBytes, manifest.Verity.SizeBytes); err != nil {
		return err
	}
	if err := writer.writePayload(
		ctx,
		targetRoles.os,
		resolved[targetRoles.os],
		fetched.Image,
		manifest.Image.SizeBytes,
		manifest.Image.Digest,
	); err != nil {
		return err
	}
	if err := writer.writePayload(
		ctx,
		targetRoles.verity,
		resolved[targetRoles.verity],
		fetched.Verity,
		manifest.Verity.SizeBytes,
		manifest.Verity.Digest,
	); err != nil {
		return err
	}
	if err := writer.configureBootTrial(ctx, plan.Value(), targetRoles, resolved); err != nil {
		return err
	}
	if err := writer.report(ctx, Progress{
		Step:      agentprotocol.StepWriteImage,
		Percent:   100,
		Completed: true,
	}); err != nil {
		return err
	}
	if err := writer.report(ctx, Progress{
		Step:      agentprotocol.StepVerifyImage,
		Percent:   100,
		Completed: true,
	}); err != nil {
		return err
	}
	return nil
}

func (writer *Writer) configureBootTrial(
	ctx context.Context,
	plan agentprotocol.Plan,
	targets payloadTargets,
	resolved map[agentprotocol.DiskRole]layout.RoleDevice,
) error {
	if plan.OperationType != agentprotocol.OperationTypeUpdate {
		return nil
	}
	if writer.bootTrial == nil {
		return errors.New("boot trial driver is required for Update")
	}
	allowed := make(map[agentprotocol.DiskRole]struct{}, len(plan.AllowedTargetRoles))
	for _, role := range plan.AllowedTargetRoles {
		allowed[role] = struct{}{}
	}
	if _, ok := allowed[agentprotocol.DiskRoleBoot]; !ok {
		return errors.New("update Plan must allow Boot role for boot trial metadata")
	}
	bootDevice, ok := resolved[agentprotocol.DiskRoleBoot]
	if !ok {
		return errors.New("partition layout does not contain role \"Boot\"")
	}
	if plan.Artifact == nil {
		return errors.New("plan artifact is required")
	}
	request := boottrial.Request{
		BootDevicePath:     bootDevice.DevicePath,
		ActiveSlot:         plan.ActiveSlot,
		TargetSlot:         slotForRole(targets.os),
		RollbackSlot:       plan.ActiveSlot,
		ArtifactGeneration: plan.Artifact.Generation,
		MaxAttempts:        boottrial.MaxAttempts,
	}
	if err := writer.bootTrial.Configure(ctx, request); err != nil {
		return fmt.Errorf("configure boot trial metadata: %w", err)
	}
	return nil
}

func (writer *Writer) cleanTargets(
	ctx context.Context,
	plan agentprotocol.ValidatedPlan,
	device disk.Device,
) error {
	if err := validateProgressSteps(plan.Value()); err != nil {
		return err
	}
	value := plan.Value()
	switch value.OperationType {
	case agentprotocol.OperationTypeWipeAll:
		if writer.sanitize != nil {
			sanitized, err := writer.sanitize.Sanitize(ctx, device.Path, device.SizeBytes)
			if err != nil {
				return fmt.Errorf("sanitize root disk: %w", err)
			}
			if sanitized {
				return writer.reportCleaningCompletion(ctx)
			}
		}
		if err := writer.zeroRole(ctx, agentprotocol.DiskRoleData, device.Path, device.SizeBytes); err != nil {
			return fmt.Errorf("wipe root disk: %w", err)
		}
	case agentprotocol.OperationTypeClean:
		if len(value.AllowedTargetRoles) == 0 {
			return writer.reportCleaningCompletion(ctx)
		}
		resolved, err := writer.layout.Prepare(ctx, agentprotocol.OperationTypeClean, device)
		if err != nil {
			return fmt.Errorf("resolve Cleaning partition layout: %w", err)
		}
		orderedRoles := slices.Clone(value.AllowedTargetRoles)
		for _, role := range orderedRoles {
			target, ok := resolved[role]
			if !ok {
				return fmt.Errorf("partition layout does not contain role %q", role)
			}
			if err := writer.zeroRole(ctx, role, target.DevicePath, target.SizeBytes); err != nil {
				return fmt.Errorf("clean role %s: %w", role, err)
			}
		}
	case agentprotocol.OperationTypeProvision, agentprotocol.OperationTypeUpdate:
		return fmt.Errorf("unsupported cleaning operation type %q", value.OperationType)
	default:
		return fmt.Errorf("unsupported operation type %q", value.OperationType)
	}
	return writer.reportCleaningCompletion(ctx)
}

func (writer *Writer) zeroRole(
	ctx context.Context,
	role agentprotocol.DiskRole,
	devicePath string,
	sizeBytes int64,
) error {
	targetDevice, err := writer.opener.Open(devicePath)
	if err != nil {
		return fmt.Errorf("open %s target: %w", role, err)
	}
	if _, err := targetDevice.Seek(0, io.SeekStart); err != nil {
		return errors.Join(fmt.Errorf("seek %s target: %w", role, err), targetDevice.Close())
	}
	writeErr := zeroAndVerify(
		ctx,
		targetDevice,
		sizeBytes,
		func(ctx context.Context, percent int) error {
			if writer.progress == nil {
				return nil
			}
			return writer.report(ctx, Progress{
				Step:     agentprotocol.StepWriteImage,
				DiskRole: role,
				Percent:  int32(percent),
			})
		},
	)
	if err := errors.Join(writeErr, targetDevice.Close()); err != nil {
		return fmt.Errorf("zero and verify %s target: %w", role, err)
	}
	return nil
}

func zeroAndVerify(
	ctx context.Context,
	target payload.SyncReadWriteSeeker,
	sizeBytes int64,
	report payload.ProgressFunc,
) error {
	if sizeBytes <= 0 {
		return errors.New("payload size must be greater than zero")
	}
	source := io.LimitReader(zeroReader{}, sizeBytes)
	expectedDigest, err := zeroDigest(sizeBytes)
	if err != nil {
		return err
	}
	return payload.WriteAndVerify(ctx, target, source, sizeBytes, expectedDigest, report)
}

func zeroDigest(sizeBytes int64) (string, error) {
	if sizeBytes <= 0 {
		return "", errors.New("payload size must be greater than zero")
	}
	hasher := sha256.New()
	buffer := make([]byte, payload.ChunkSize)
	remaining := sizeBytes
	for remaining > 0 {
		chunk := min(int64(len(buffer)), remaining)
		if _, err := hasher.Write(buffer[:chunk]); err != nil {
			return "", fmt.Errorf("hash zero payload: %w", err)
		}
		remaining -= chunk
	}
	return digest.NewDigest(digest.SHA256, hasher).String(), nil
}

type zeroReader struct{}

func (zeroReader) Read(target []byte) (int, error) {
	clear(target)
	return len(target), nil
}

func (writer *Writer) reportCleaningCompletion(ctx context.Context) error {
	if err := writer.report(ctx, Progress{
		Step:      agentprotocol.StepWriteImage,
		Percent:   100,
		Completed: true,
	}); err != nil {
		return err
	}
	if err := writer.report(ctx, Progress{
		Step:      agentprotocol.StepVerifyImage,
		Percent:   100,
		Completed: true,
	}); err != nil {
		return err
	}
	return nil
}

func validateProgressSteps(plan agentprotocol.Plan) error {
	required := map[string]bool{
		agentprotocol.StepWriteImage:  false,
		agentprotocol.StepVerifyImage: false,
	}
	for _, step := range plan.Steps {
		if _, ok := required[step.Name]; ok {
			required[step.Name] = true
		}
	}
	for _, step := range []string{agentprotocol.StepWriteImage, agentprotocol.StepVerifyImage} {
		if !required[step] {
			return fmt.Errorf("plan does not contain required progress step %q", step)
		}
	}
	return nil
}

type payloadTargets struct {
	os     agentprotocol.DiskRole
	verity agentprotocol.DiskRole
}

func selectPayloadTargets(plan agentprotocol.ValidatedPlan) (payloadTargets, error) {
	value := plan.Value()
	var expected payloadTargets
	switch value.OperationType {
	case agentprotocol.OperationTypeProvision:
		expected = payloadTargets{os: agentprotocol.DiskRoleOSA, verity: agentprotocol.DiskRoleVerityA}
	case agentprotocol.OperationTypeUpdate:
		switch value.ActiveSlot {
		case "A":
			expected = payloadTargets{os: agentprotocol.DiskRoleOSB, verity: agentprotocol.DiskRoleVerityB}
		case "B":
			expected = payloadTargets{os: agentprotocol.DiskRoleOSA, verity: agentprotocol.DiskRoleVerityA}
		default:
			return payloadTargets{}, fmt.Errorf("unsupported active slot %q", value.ActiveSlot)
		}
	case agentprotocol.OperationTypeClean, agentprotocol.OperationTypeWipeAll:
		return payloadTargets{}, fmt.Errorf("operation type %q does not write OS payloads", value.OperationType)
	default:
		return payloadTargets{}, fmt.Errorf("unsupported operation type %q", value.OperationType)
	}

	allowed := make(map[agentprotocol.DiskRole]struct{}, len(value.AllowedTargetRoles))
	for _, role := range value.AllowedTargetRoles {
		allowed[role] = struct{}{}
	}
	if _, ok := allowed[expected.os]; !ok {
		return payloadTargets{}, fmt.Errorf("plan does not allow target role %q", expected.os)
	}
	if _, ok := allowed[expected.verity]; !ok {
		return payloadTargets{}, fmt.Errorf("plan does not allow target role %q", expected.verity)
	}
	return expected, nil
}

func slotForRole(role agentprotocol.DiskRole) string {
	switch role {
	case agentprotocol.DiskRoleBoot,
		agentprotocol.DiskRoleVerityA,
		agentprotocol.DiskRoleVerityB,
		agentprotocol.DiskRoleState,
		agentprotocol.DiskRoleData:
		return ""
	case agentprotocol.DiskRoleOSA:
		return "A"
	case agentprotocol.DiskRoleOSB:
		return "B"
	default:
		return ""
	}
}

func validateProfileCapacity(targets payloadTargets, imageSize, veritySize int64) error {
	capacities := make(map[agentprotocol.DiskRole]int64, len(layout.Definitions()))
	for _, definition := range layout.Definitions() {
		capacities[definition.Role] = definition.MinimumSizeBytes
	}
	checks := []struct {
		role agentprotocol.DiskRole
		size int64
	}{
		{role: targets.os, size: imageSize},
		{role: targets.verity, size: veritySize},
	}
	for _, check := range checks {
		if check.size > capacities[check.role] {
			return fmt.Errorf(
				"payload for role %q has %d bytes, profile capacity is %d",
				check.role,
				check.size,
				capacities[check.role],
			)
		}
	}
	return nil
}

func validateCapacity(
	resolved map[agentprotocol.DiskRole]layout.RoleDevice,
	targets payloadTargets,
	imageSize, veritySize int64,
) error {
	checks := []struct {
		role agentprotocol.DiskRole
		size int64
	}{
		{role: targets.os, size: imageSize},
		{role: targets.verity, size: veritySize},
	}
	for _, check := range checks {
		target, ok := resolved[check.role]
		if !ok {
			return fmt.Errorf("partition layout does not contain role %q", check.role)
		}
		if check.size > target.SizeBytes {
			return fmt.Errorf(
				"payload for role %q has %d bytes, target capacity is %d",
				check.role,
				check.size,
				target.SizeBytes,
			)
		}
	}
	return nil
}

func (writer *Writer) writePayload(
	ctx context.Context,
	role agentprotocol.DiskRole,
	target layout.RoleDevice,
	source artifactfetch.Payload,
	size int64,
	digest string,
) error {
	sourceReader, err := source.Open(ctx)
	if err != nil {
		return fmt.Errorf("open %s payload: %w", role, err)
	}
	targetDevice, err := writer.opener.Open(target.DevicePath)
	if err != nil {
		closeErr := sourceReader.Close()
		return errors.Join(fmt.Errorf("open %s target: %w", role, err), closeErr)
	}
	if _, err := targetDevice.Seek(0, io.SeekStart); err != nil {
		return errors.Join(
			fmt.Errorf("seek %s target: %w", role, err),
			sourceReader.Close(),
			targetDevice.Close(),
		)
	}
	writeErr := payload.WriteAndVerify(
		ctx,
		targetDevice,
		sourceReader,
		size,
		digest,
		func(ctx context.Context, percent int) error {
			if writer.progress == nil {
				return nil
			}
			return writer.report(ctx, Progress{
				Step:     agentprotocol.StepWriteImage,
				DiskRole: role,
				Percent:  int32(percent),
			})
		},
	)
	sourceCloseErr := sourceReader.Close()
	targetCloseErr := targetDevice.Close()
	if err := errors.Join(writeErr, sourceCloseErr, targetCloseErr); err != nil {
		return fmt.Errorf("write and verify %s payload: %w", role, err)
	}
	return nil
}

func (writer *Writer) report(ctx context.Context, progress Progress) error {
	if writer.progress == nil {
		return nil
	}
	if err := writer.progress(ctx, progress); err != nil {
		return fmt.Errorf("report %s progress: %w", progress.Step, err)
	}
	return nil
}

type LinuxDeviceOpener struct{}

func (LinuxDeviceOpener) Open(path string) (SyncReadWriteSeekCloser, error) {
	return os.OpenFile(path, os.O_RDWR, 0)
}
