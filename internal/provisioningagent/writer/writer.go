package writer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/walnuts1018/cluster-api-provider-tart/internal/provisioningagent/artifactfetch"
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

type Progress struct {
	Step      string
	DiskRole  agentprotocol.DiskRole
	Percent   int32
	Completed bool
}

type ProgressReporter func(context.Context, Progress) error

type Writer struct {
	layout   LayoutPreparer
	fetcher  ArtifactFetcher
	opener   DeviceOpener
	progress ProgressReporter
}

func New(
	layoutPreparer LayoutPreparer,
	fetcher ArtifactFetcher,
	opener DeviceOpener,
	progress ProgressReporter,
) *Writer {
	return &Writer{
		layout:   layoutPreparer,
		fetcher:  fetcher,
		opener:   opener,
		progress: progress,
	}
}

func (writer *Writer) WriteTargets(
	ctx context.Context,
	plan agentprotocol.ValidatedPlan,
	device disk.Device,
) error {
	if err := validateProgressSteps(plan.Value()); err != nil {
		return err
	}
	targetRoles, err := selectPayloadTargets(plan)
	if err != nil {
		return err
	}
	fetched, err := writer.fetcher.Fetch(ctx, plan.Value().Artifact, layout.ProfileID)
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
